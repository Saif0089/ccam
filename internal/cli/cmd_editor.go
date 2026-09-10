package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/editors"
	"ccam/internal/service"
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
	var withExt []editors.Editor
	for _, ed := range installed {
		if ed.HasExtension {
			withExt = append(withExt, ed)
		}
	}
	if len(installed) == 0 {
		fmt.Println("No VS Code-family editor found on this machine.")
		return 0
	}
	if len(withExt) == 0 {
		fmt.Println("No editor here has the Claude Code extension installed, so there is nothing to point at an account.")
		return 0
	}

	if len(args) == 0 {
		reportEditors(installed, list, accountsDir)
		return 0
	}

	acct, ok := switching.ResolveAccount(list, args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "ccam: no account %q\n", args[0])
		return 1
	}
	self, err := service.SelfPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccam: cannot find my own binary:", err)
		return 1
	}

	// The account every new conversation starts as. The wrapper reads this when
	// the editor launches Claude; from there, `ccam <name>` typed in one chat
	// moves that chat alone.
	if err := writeEditorDefault(accountsDir, acct); err != nil {
		fmt.Fprintln(os.Stderr, "ccam:", err)
		return 1
	}

	failed := false
	for _, ed := range withExt {
		if err := editors.PointAtWrapper(ed.Settings, self); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: could not update settings.json: %v\n", ed.Name, err)
			failed = true
			continue
		}
		fmt.Printf("  %s → %s\n", ed.Name, displayName(acct))
	}
	for _, ed := range installed {
		if !ed.HasExtension {
			fmt.Printf("  %s: skipped, no Claude Code extension installed\n", ed.Name)
		}
	}
	if failed {
		return 1
	}
	fmt.Println("\nNew conversations start on this account. In any of them, type `ccam <name>`")
	fmt.Println("to move that conversation — and only that one — to another account.")
	fmt.Println("Conversations already open keep the account they started with.")
	return 0
}

// writeEditorDefault records the account an editor's conversations start as.
func writeEditorDefault(accountsDir string, acct accounts.Account) error {
	dir := filepath.Join(filepath.Dir(accountsDir), "editors")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(storeOwner{
		AccountID: acct.ID,
		Name:      displayName(acct),
		ConfigDir: acct.ConfigDir,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "default.json"), data, 0o600)
}

// reportEditors says what each editor is set to.
func reportEditors(installed []editors.Editor, list []accounts.Account, accountsDir string) {
	def := "not set"
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(accountsDir), "editors", "default.json")); err == nil {
		var rec storeOwner
		if json.Unmarshal(data, &rec) == nil && rec.Name != "" {
			def = rec.Name
		}
	}
	for _, ed := range installed {
		switch {
		case !ed.HasExtension:
			fmt.Printf("  %-18s no Claude Code extension installed\n", ed.Name)
		case editors.WrapperPath(ed.Settings) == "":
			fmt.Printf("  %-18s not managed by ccam (uses your default login)\n", ed.Name)
		default:
			fmt.Printf("  %-18s new conversations start as %s\n", ed.Name, def)
		}
	}
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
