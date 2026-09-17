package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/panel"
	"ccam/internal/shellrc"
)

// defaultPanelAddr is the panel's own port, one above the local UI's. It binds
// loopback by default: a panel is only useful to other people once it is
// deliberately exposed, and defaulting to that would put every login it holds
// on the network the moment someone tried it out.
const defaultPanelAddr = "127.0.0.1:47933"

func cmdPanel(args []string) int {
	if len(args) == 0 {
		panelUsage(os.Stderr)
		return 1
	}
	switch args[0] {
	case "serve":
		return panelServe(args[1:])
	case "join":
		return panelJoin(args[1:])
	case "check":
		return panelCheck(args[1:])
	case "push":
		return panelPush(args[1:])
	case "genkey":
		return panelGenkey()
	case "help", "--help", "-h":
		panelUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "ccam panel: unknown command %q\n\n", args[0])
		panelUsage(os.Stderr)
		return 1
	}
}

func panelUsage(w *os.File) {
	fmt.Fprint(w, `ccam panel — lend Claude logins out, and take them back

  ccam panel serve [--addr host:port]   run the panel (default `+defaultPanelAddr+`)
  ccam panel join <url> <code>          enrol this machine with a panel
  ccam panel check                      ask the panel what this machine holds, now
  ccam panel push <account> <url>       store an account's login in the panel
  ccam panel genkey                     print a new sealing key for a hosted panel

Serving on 127.0.0.1 keeps the panel to this machine. To let other people
reach it, give --addr an address they can see, and put it behind TLS.
`)
}

func panelPaths() (store, key, client string, err error) {
	base, err := config.HomeDir()
	if err != nil {
		return "", "", "", err
	}
	return filepath.Join(base, "panel.json"),
		filepath.Join(base, "panel.key"),
		filepath.Join(base, "panel-client.json"), nil
}

func panelServe(args []string) int {
	addr := defaultPanelAddr
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			addr = args[i+1]
			i++
		}
	}
	storePath, keyPath, _, err := panelPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	secret, err := panel.LoadSecret(keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	srv := panel.NewServer(panel.NewStore(storePath), secret)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccam: cannot listen on %s: %v\n", addr, err)
		return 1
	}
	fmt.Printf("ccam panel on http://%s\n", ln.Addr())
	if !strings.HasPrefix(addr, "127.0.0.1") && !strings.HasPrefix(addr, "localhost") {
		fmt.Println("This panel is reachable from the network. Put it behind TLS before anyone signs in over it.")
	}
	if err := http.Serve(ln, srv.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	return 0
}

func panelJoin(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ccam panel join <url> <code>")
		return 1
	}
	server, code := strings.TrimRight(args[0], "/"), args[1]
	_, _, clientPath, err := panelPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	name, _ := os.Hostname()
	if name == "" {
		name = "a machine"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := panel.Enroll(ctx, server, code, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if err := panel.SaveClientConfig(clientPath, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	fmt.Printf("This machine is enrolled with %s as %q.\n", server, name)
	return panelCheck(nil)
}

// panelClient builds the check-in client over this machine's real accounts.
func panelClient() (*panel.Client, error) {
	_, _, clientPath, err := panelPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := panel.LoadClientConfig(clientPath)
	if err != nil {
		return nil, err
	}
	accountsFile, err := config.AccountsFile()
	if err != nil {
		return nil, err
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		return nil, err
	}
	mgr := accounts.NewManager(accounts.NewStore(accountsFile), accountsDir)
	return &panel.Client{
		Config:   cfg,
		Accounts: mgr,
		AfterChange: func() {
			// An account gained or given back changes which shell commands
			// exist, so the rc blocks have to follow it.
			home, err := os.UserHomeDir()
			if err != nil {
				return
			}
			list, err := mgr.List()
			if err != nil {
				return
			}
			entries := make([]shellrc.AliasEntry, 0, len(list))
			for _, a := range list {
				if a.IsDefault() {
					continue // `claude` is already that account
				}
				entries = append(entries, shellrc.AliasEntry{Alias: a.Alias, ConfigDir: a.ConfigDir, Account: a.Slug})
			}
			_ = shellrc.NewSyncer(home).Sync(entries)
		},
	}, nil
}

func panelCheck(_ []string) int {
	c, err := panelClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if !c.Config.Configured() {
		fmt.Fprintln(os.Stderr, "ccam: this machine is not enrolled with a panel. Run `ccam panel join <url> <code>`.")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	change, err := c.CheckIn(ctx)
	for _, name := range change.Gained {
		fmt.Printf("You now have %s.\n", name)
	}
	for _, name := range change.Lost {
		fmt.Printf("%s went back to the panel.\n", name)
	}
	if errors.Is(err, panel.ErrNotEnrolled) {
		fmt.Fprintln(os.Stderr, "ccam: this machine is no longer enrolled with the panel.")
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if change.Empty() {
		fmt.Println("Nothing changed.")
	}
	return 0
}

// panelPush stores an account's login in the panel, from the machine where that
// account is signed in. The panel cannot sign an account in by itself, so this
// is how an account gets something to lend.
func panelPush(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ccam panel push <account> <url>")
		return 1
	}
	name, server := args[0], strings.TrimRight(args[1], "/")

	list, err := loadAccounts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	var acct accounts.Account
	for _, a := range list {
		if strings.EqualFold(a.Slug, name) || strings.EqualFold(a.Name, name) {
			acct = a
		}
	}
	if acct.ID == "" {
		fmt.Fprintf(os.Stderr, "ccam: no account called %q on this machine.\n", name)
		return 1
	}
	raw, err := os.ReadFile(filepath.Join(acct.ConfigDir, ".credentials.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ccam: no login file for %s. Claude Code may be keeping it in the macOS Keychain,\n", acct.Name)
		fmt.Fprintln(os.Stderr, "      which ccam deliberately does not read. Sign this account in on a machine that")
		fmt.Fprintln(os.Stderr, "      writes the file, or run the panel there.")
		return 1
	}

	fmt.Fprint(os.Stderr, "Panel password: ")
	pw, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	jar := &oneHostJar{}
	httpc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := panel.AdminLogin(ctx, httpc, server, strings.TrimSpace(pw)); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	id, err := panel.FindAccountID(ctx, httpc, server, acct.Name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	if err := panel.PushLogin(ctx, httpc, server, id, base64.StdEncoding.EncodeToString(raw)); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	fmt.Printf("%s can now be lent out.\n", acct.Name)
	return 0
}

// oneHostJar holds the panel's session cookie for the life of one command.
// net/http/cookiejar needs a public-suffix list, which is a large dependency
// for one cookie against one host.
type oneHostJar struct{ cookies []*http.Cookie }

func (j *oneHostJar) SetCookies(_ *neturl.URL, c []*http.Cookie) { j.cookies = c }
func (j *oneHostJar) Cookies(_ *neturl.URL) []*http.Cookie       { return j.cookies }

// checkInEvery is how often an enrolled machine asks the panel what it holds.
// It is the worst case for how long someone keeps an account after it was
// taken back, so it is short; the request is tiny and answered from a file.
const checkInEvery = 30 * time.Second

// watchPanel keeps this machine in step with the panel it is enrolled with, for
// as long as ccam is running. It is silent when nothing changes, which is
// almost always, and gives up quietly when this machine answers to no panel.
func watchPanel(ctx context.Context) {
	c, err := panelClient()
	if err != nil || !c.Config.Configured() {
		return
	}
	t := time.NewTicker(checkInEvery)
	defer t.Stop()
	for {
		change, err := c.CheckIn(ctx)
		for _, name := range change.Gained {
			fmt.Printf("ccam: %s was assigned to this machine.\n", name)
		}
		for _, name := range change.Lost {
			fmt.Printf("ccam: %s went back to the panel, and its login has been removed.\n", name)
		}
		if errors.Is(err, panel.ErrNotEnrolled) {
			fmt.Fprintln(os.Stderr, "ccam: this machine is no longer enrolled with the panel; everything it lent has been given back.")
			return
		}
		// Any other failure is the panel being unreachable, which is not an
		// event: a machine that cannot ask keeps what it was last told it had.
		// Saying so every thirty seconds would fill the log with the fact that
		// a laptop is on a train.
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// panelGenkey prints a fresh sealing key for a panel that runs somewhere with no
// disk of its own — a serverless deployment. The value goes in that host's
// environment as CCAM_PANEL_KEY, and is the only thing that can open the logins
// the panel holds, so it is printed once and never kept by ccam.
func panelGenkey() int {
	key, err := panel.GenerateKeyBase64()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	fmt.Println(key)
	fmt.Fprintln(os.Stderr, "Set this as CCAM_PANEL_KEY in the panel's environment. Keep it — it cannot be recovered,")
	fmt.Fprintln(os.Stderr, "and losing it makes every stored login unreadable.")
	return 0
}
