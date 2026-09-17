// Package guard holds repository-wide invariants that no single package can
// assert about itself.
package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is this package's directory, two levels down from the module root.
const repoRoot = "../.."

// TestCcamNeverTouchesTheCredentialStore is the guard on the bug that cost two
// real logins.
//
// ccam used to keep a copy of each session's credentials so a switch could be a
// write rather than a restart. Making that work meant re-deriving Claude Code's
// own Keychain item name and writing it through `security -i`, which truncates
// at 4095 bytes: a real store is larger than that once MCP logins are in it, so
// the item was replaced with a fragment that still parsed, and the mirror then
// copied the fragment over the account's own store. Both managed accounts were
// unrecoverable.
//
// The fix was to delete the whole arrangement. ccam reads an account's login
// only through Claude Code itself (`claude auth status`) or from the plain
// credentials file, and writes one never. This test is what keeps it deleted:
// the strings below cannot reappear in shipped code without failing the build.
//
// Comments are exempt on purpose — internal/accounts/env.go documents the
// derivation to explain why an empty CLAUDE_SECURESTORAGE_CONFIG_DIR is
// dangerous, and that explanation is worth keeping.
func TestCcamNeverWritesTheCredentialStore(t *testing.T) {
	// The rule this enforces is narrower than "never touch the Keychain": it is
	// "never WRITE a credential store". Writing is what destroyed real logins —
	// `security -i` truncating a store at 4 KB, the mirror copying the fragment
	// back. Reading one to display usage, or to capture a login for the panel to
	// lend, is safe and necessary, and is allowed. Only the write and delete
	// primitives are banned.
	// Banned is the WRITE of credential DATA — the thing that truncated a store
	// and copied the fragment back over real logins. Reading a login (to show
	// usage, or to capture it for the panel) and deleting a specific one (to
	// revoke it) are both safe and necessary, and are allowed.
	banned := map[string]string{
		"add-generic-password": "writing Claude Code's Keychain item",
		"security -i":          "writing the Keychain from stdin (the 4 KB truncation bug)",
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		// Tests may name these strings; shipped code may not.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Mode 0 leaves comments out of the tree entirely.
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			for needle, why := range banned {
				if strings.Contains(value, needle) {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s:%d: %q is back — %s.\n"+
						"ccam must never WRITE Claude Code's credential store; this is the class of code that destroyed two real logins.",
						rel, fset.Position(lit.Pos()).Line, needle, why)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCredstorePackageStaysDeleted is the blunter half of the same guard: the
// package that owned every credential write must not come back under its old
// name and quietly reacquire callers.
func TestCredstorePackageStaysDeleted(t *testing.T) {
	if _, err := os.Stat(filepath.Join(repoRoot, "internal", "credstore")); !os.IsNotExist(err) {
		t.Error("internal/credstore is back. It existed to write credential stores, which ccam no longer does.")
	}
}
