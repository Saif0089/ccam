package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"clawdh/internal/buildinfo"
	"clawdh/internal/claudebin"
	"clawdh/internal/config"
	"clawdh/internal/service"
	"clawdh/panel"
)

// cmdRemote turns this machine's remote help on or off. Remote help lets the
// panel ask this machine to look at itself — diagnose a problem, list its
// sessions, send a transcript for debugging — and nothing of the sort can
// happen until the person here turns it on. Every request that does run is
// printed, so it is never silent.
func cmdRemote(args []string) int {
	_, _, clientPath, err := panelPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdh:", err)
		return 1
	}
	cfg, err := panel.LoadClientConfig(clientPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdh:", err)
		return 1
	}

	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch action {
	case "on", "off":
		if !cfg.Configured() {
			fmt.Println("This machine isn't connected to a panel, so there's nothing to turn on.")
			fmt.Printf("Connect it first from the clawdh page (http://127.0.0.1:%d) or `clawdh join <link>`.\n", config.DefaultPort)
			return 1
		}
		cfg.Remote = action == "on"
		if err := panel.SaveClientConfig(clientPath, cfg); err != nil {
			fmt.Fprintln(os.Stderr, "clawdh:", err)
			return 1
		}
		if cfg.Remote {
			fmt.Println("Remote help is on. The panel can now ask this machine to diagnose itself,")
			fmt.Println("list its sessions, or send a transcript — and every request is printed here.")
			fmt.Println("Turn it off any time with `clawdh remote off`.")
		} else {
			fmt.Println("Remote help is off. The panel can no longer ask this machine for anything.")
		}
		return 0
	case "status", "":
		if !cfg.Configured() {
			fmt.Println("This machine isn't connected to a panel.")
			return 0
		}
		if cfg.Remote {
			fmt.Println("Remote help is ON — the panel may ask this machine to diagnose itself, list its")
			fmt.Println("sessions, or send a transcript. Each request is printed. Turn off: `clawdh remote off`.")
		} else {
			fmt.Println("Remote help is OFF. Turn on with `clawdh remote on` to let the panel ask this")
			fmt.Println("machine to look at itself (diagnose / list sessions / send a transcript).")
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: clawdh remote [on|off|status]")
		return 2
	}
}

// runRemoteJobs executes the consented jobs a check-in handed back and posts
// each answer to the panel. Every job is announced as it runs, so remote help
// is visible on the machine it acts on — the person sees exactly what was asked.
func runRemoteJobs(ctx context.Context, c *panel.Client, jobs []panel.RemoteJob) {
	for _, j := range jobs {
		fmt.Printf("clawdh: the panel asked this machine to %s — running it.\n", describeJob(j))
		notifyBrief("This machine was asked to " + describeJob(j))
		result, status := executeJob(j)
		if err := c.ReportResult(ctx, j.ID, status, result); err != nil {
			fmt.Fprintf(os.Stderr, "clawdh: could not send the result of %q back to the panel: %v\n", j.Kind, err)
			continue
		}
		fmt.Printf("clawdh: sent the %s result to the panel.\n", j.Kind)
	}
}

func describeJob(j panel.RemoteJob) string {
	switch j.Kind {
	case "diagnose":
		return "check its own health"
	case "sessions":
		return "list its sessions"
	case "transcript":
		return "send a session transcript"
	case "ls":
		return "list " + niceParams(j.Params)
	case "get":
		return "send " + niceParams(j.Params)
	default:
		return j.Kind
	}
}

func niceParams(p string) string {
	if strings.TrimSpace(p) == "" {
		return "the home folder"
	}
	return p
}

// executeJob runs one read-only job and returns its result text and a status
// ("done" or "error"). Nothing here changes the machine.
func executeJob(j panel.RemoteJob) (result, status string) {
	switch j.Kind {
	case "diagnose":
		return jobDiagnose(), "done"
	case "sessions":
		return jobSessions(), "done"
	case "transcript":
		out, err := jobTranscript(j.Params)
		if err != nil {
			return err.Error(), "error"
		}
		return out, "done"
	case "ls":
		return jobLs(j.Params)
	case "get":
		return jobGet(j.Params)
	default:
		return "This machine doesn't know how to " + j.Kind + ".", "error"
	}
}

// expandPath resolves a browse path: empty or "~" is the home folder, "~/x" is
// under it, anything else is taken as-is (the file browser navigates by absolute
// path). Read-only either way.
func expandPath(p string) string {
	p = strings.TrimSpace(p)
	home, _ := os.UserHomeDir()
	switch {
	case p == "" || p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	default:
		return p
	}
}

// jobLs lists a folder's entries — the file browser's navigation. Names, sizes,
// mod times and dir/file only; never any contents. Result is JSON the panel
// renders. Bounded so an enormous folder can't be dragged through whole.
func jobLs(p string) (string, string) {
	dir := expandPath(p)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err.Error(), "error"
	}
	type ent struct {
		Name string `json:"name"`
		Dir  bool   `json:"dir"`
		Size int64  `json:"size"`
		Mod  string `json:"mod"`
	}
	out := struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		Entries []ent  `json:"entries"`
	}{Path: dir, Parent: filepath.Dir(dir)}
	for i, e := range entries {
		if i >= 2000 {
			break
		}
		var size int64
		var mod string
		if info, err := e.Info(); err == nil {
			size, mod = info.Size(), info.ModTime().Format("2006-01-02 15:04")
		}
		out.Entries = append(out.Entries, ent{Name: e.Name(), Dir: e.IsDir(), Size: size, Mod: mod})
	}
	b, _ := json.Marshal(out)
	return string(b), "done"
}

// maxGetBytes caps a fetched file, so a huge one can't be dragged whole through
// the panel.
const maxGetBytes = 4 << 20

// jobGet reads one file and ships it back. Text is sent as-is; anything not
// valid UTF-8 is base64'd, so any file survives the JSON round-trip.
func jobGet(p string) (string, string) {
	path := expandPath(p)
	info, err := os.Stat(path)
	if err != nil {
		return err.Error(), "error"
	}
	if info.IsDir() {
		return "that is a folder, not a file", "error"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error(), "error"
	}
	truncated := false
	if len(data) > maxGetBytes {
		data, truncated = data[:maxGetBytes], true
	}
	enc, content := "utf8", string(data)
	if !utf8.Valid(data) {
		enc, content = "base64", base64.StdEncoding.EncodeToString(data)
	}
	out := struct {
		Path      string `json:"path"`
		Size      int64  `json:"size"`
		Encoding  string `json:"encoding"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}{path, info.Size(), enc, content, truncated}
	b, _ := json.Marshal(out)
	return string(b), "done"
}

// jobDiagnose reports whether clawdh is healthy here and can reach the gateway —
// the machine-side half of "why isn't it working for them?".
func jobDiagnose() string {
	var b strings.Builder
	fmt.Fprintf(&b, "clawdh %s on %s\n", buildinfo.Version, runtimeHost())

	if info, err := service.Running(); err == nil && info != nil {
		fmt.Fprintf(&b, "service: running (port %d, pid %d, version %s)\n", info.Port, info.PID, info.Version)
	} else {
		fmt.Fprintf(&b, "service: NOT running — `clawdh start` would bring it back\n")
	}

	if claude := claudebin.Resolve(); claude != "" {
		fmt.Fprintf(&b, "claude binary: %s\n", claude)
	} else {
		fmt.Fprintf(&b, "claude binary: not found on PATH\n")
	}

	shares := loadSharesForDiag()
	fmt.Fprintf(&b, "shared accounts: %d\n", len(shares))
	if len(shares) > 0 {
		gw := shares[0].Gateway
		fmt.Fprintf(&b, "gateway: %s — %s\n", gw, reachable(gw))
	} else {
		fmt.Fprintf(&b, "gateway: none configured (no accounts shared with this machine yet)\n")
	}
	return b.String()
}

// jobSessions lists this machine's Claude Code sessions — ids, project and size
// only, never their content — so an admin can point a transcript request at one.
func jobSessions() string {
	files := sessionFiles()
	if len(files) == 0 {
		return "No Claude Code sessions on this machine."
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if len(files) > 50 {
		files = files[:50]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d recent session(s):\n", len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "  %s  %6s  %s  (%s)\n", f.id, humanSize(f.size), f.project, f.mod.Format("2006-01-02 15:04"))
	}
	b.WriteString("\nAsk for one with a transcript request, using its id.")
	return b.String()
}

// maxTranscriptBytes caps a returned transcript, so a giant session can't be
// dragged whole through the panel; the tail is what a recent problem is in.
const maxTranscriptBytes = 512 << 10

// jobTranscript returns one named session's transcript (its tail, if large).
func jobTranscript(sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", fmt.Errorf("no session id was given")
	}
	// The id is a file stem; keep it to a single path element so it can't escape
	// the projects tree.
	if strings.ContainsAny(sessionID, "/\\") || strings.Contains(sessionID, "..") {
		return "", fmt.Errorf("that is not a valid session id")
	}
	matches, _ := filepath.Glob(filepath.Join(claudeProjectsDir(), "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no session %q on this machine", sessionID)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", fmt.Errorf("could not read that session: %w", err)
	}
	if len(data) > maxTranscriptBytes {
		return "…(truncated to the last " + humanSize(maxTranscriptBytes) + ")\n" + string(data[len(data)-maxTranscriptBytes:]), nil
	}
	return string(data), nil
}

// --- small helpers ---------------------------------------------------------

type sessionFile struct {
	id, project string
	size        int64
	mod         time.Time
}

func claudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func sessionFiles() []sessionFile {
	root := claudeProjectsDir()
	matches, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	var out []sessionFile
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		out = append(out, sessionFile{
			id:      strings.TrimSuffix(filepath.Base(m), ".jsonl"),
			project: filepath.Base(filepath.Dir(m)),
			size:    info.Size(),
			mod:     info.ModTime(),
		})
	}
	return out
}

func loadSharesForDiag() []panel.GatewayShare {
	path, err := config.SharesFile()
	if err != nil {
		return nil
	}
	shares, _ := panel.LoadShares(path)
	return shares
}

// reachable reports, in words, whether the gateway answers — any HTTP response
// (even a 401) means the network path is good; only a transport error is a
// problem the person needs to know about.
func reachable(url string) string {
	if url == "" {
		return "no URL"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "UNREACHABLE (" + err.Error() + ")"
	}
	defer resp.Body.Close()
	return fmt.Sprintf("reachable (HTTP %d)", resp.StatusCode)
}

func runtimeHost() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "this machine"
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
