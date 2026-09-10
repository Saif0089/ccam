package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/credstore"
	"ccam/internal/editors"
	"ccam/internal/switching"
)

// cmdEditor points VS Code and its relatives at a ccam account.
//
// The Claude Code extension never sees the user's shell — it spawns Claude
// itself — so aliases and ccam's `claude` function do nothing for it. What it
// does read is its own `claudeCode.environmentVariables` setting, applied over
// the environment of every Claude process it starts. ccam puts one entry there,
// pointing at a credential store it owns, and from then on switching that
// editor's account is a write to that store: conversations already open pick it
// up the same way a terminal session does, because it is the same mechanism.
func cmdEditor(args []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsFile, err := config.AccountsFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	accountsDir, err := config.AccountsDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}
	list, err := accounts.NewStore(accountsFile).Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	installed := editors.Installed(home)
	if len(installed) == 0 {
		fmt.Println("No VS Code-family editor found on this machine.")
		return 0
	}

	if len(args) == 0 {
		reportEditors(installed, list)
		return 0
	}

	acct, ok := switching.ResolveAccount(list, args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "ccam: no account %q\n", args[0])
		return 1
	}

	failed := false
	for _, ed := range installed {
		storeDir := editorStoreDir(accountsDir, ed.Name)
		if err := os.MkdirAll(storeDir, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", ed.Name, err)
			failed = true
			continue
		}
		if err := credstore.Copy(acct.ConfigDir, storeDir); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: could not copy this account's login: %v\n", ed.Name, err)
			failed = true
			continue
		}
		// The usage monitor sees this directory as a session's owner and has no
		// way to know whose it is; leave it a note.
		writeStoreOwner(storeDir, acct)
		if err := editors.PointAt(ed.Settings, accounts.SecureStorageEnvVar, storeDir); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: could not update settings.json: %v\n", ed.Name, err)
			failed = true
			continue
		}
		fmt.Printf("  %s → %s\n", ed.Name, displayName(acct))
	}
	if failed {
		return 1
	}
	fmt.Println("\nConversations already open switch as soon as the extension next checks;")
	fmt.Println("a new conversation starts on this account outright.")
	return 0
}

// reportEditors says which account each editor is on, by matching what its
// store holds against every account ccam knows.
func reportEditors(installed []editors.Editor, list []accounts.Account) {
	for _, ed := range installed {
		storeDir := editors.ReadStoreDir(ed.Settings, accounts.SecureStorageEnvVar)
		switch {
		case storeDir == "":
			fmt.Printf("  %-18s not managed by ccam (uses your default login)\n", ed.Name)
		default:
			fmt.Printf("  %-18s %s\n", ed.Name, accountHolding(storeDir, list))
		}
	}
}

// accountHolding names the account whose login a store currently holds.
func accountHolding(storeDir string, list []accounts.Account) string {
	for _, a := range list {
		if credstore.Same(a.ConfigDir, storeDir) {
			return displayName(a)
		}
	}
	if owner := readStoreOwner(storeDir); owner != "" {
		return owner + " (last set by ccam)"
	}
	return "an account ccam cannot identify"
}

// editorStoreDir is where an editor's credential store lives. One per editor,
// so two editors can be on two accounts at once.
func editorStoreDir(accountsDir, editorName string) string {
	slug := strings.ToLower(strings.ReplaceAll(editorName, " ", "-"))
	return filepath.Join(filepath.Dir(accountsDir), "editors", slug)
}

// storeOwner records which account a store was last filled from, for the
// benefit of anything that finds the directory later — ccam's own reporting,
// and the usage monitor, which otherwise sees a directory it cannot place.
type storeOwner struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	ConfigDir string `json:"configDir"`
}

func writeStoreOwner(storeDir string, acct accounts.Account) {
	data, err := json.MarshalIndent(storeOwner{
		AccountID: acct.ID,
		Name:      displayName(acct),
		ConfigDir: acct.ConfigDir,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(storeDir, "owner.json"), data, 0o600)
}

func readStoreOwner(storeDir string) string {
	data, err := os.ReadFile(filepath.Join(storeDir, "owner.json"))
	if err != nil {
		return ""
	}
	var o storeOwner
	if err := json.Unmarshal(data, &o); err != nil {
		return ""
	}
	return o.Name
}
