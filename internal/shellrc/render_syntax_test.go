package shellrc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The managed block is sourced by the user's login shell on every start: a
// syntax error in it does not break ccam, it breaks their shell. Parse what we
// actually render with the real shells, where they are installed.
func TestRenderedBlockParsesInTheShellItTargets(t *testing.T) {
	// Aliases are slugified, so they are tame — but ConfigDir carries the
	// user's home directory, which is not: apostrophes, spaces and quotes all
	// occur in real macOS and Windows home paths.
	entries := []AliasEntry{
		{Alias: "claude-work", ConfigDir: "/home/me/.ccam/accounts/work", Account: "work"},
		{Alias: "claude-obrien", ConfigDir: `/Users/o'brien/.ccam/accounts/work`, Account: "obrien"},
		{Alias: "claude-spaced", ConfigDir: `/Users/a b/dir with spaces/and"quote`, Account: "spaced"},
		// No account: the direct-launch fallback has to parse too.
		{Alias: "claude-legacy", ConfigDir: "/home/me/.ccam/accounts/legacy"},
	}

	cases := []struct {
		shell Shell
		bin   string
		args  []string // args that parse a file without running it
		ext   string
	}{
		{Bash, "bash", []string{"-n"}, "sh"},
		{Zsh, "zsh", []string{"-n"}, "zsh"},
		{Fish, "fish", []string{"--no-execute"}, "fish"},
	}
	for _, c := range cases {
		bin, err := exec.LookPath(c.bin)
		if err != nil {
			t.Logf("%s not installed, skipping", c.bin)
			continue
		}
		path := filepath.Join(t.TempDir(), "block."+c.ext)
		if err := os.WriteFile(path, []byte(RenderBody(c.shell, entries)), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, append(append([]string{}, c.args...), path)...).CombinedOutput()
		if err != nil {
			t.Errorf("%s cannot parse the block ccam writes: %v\n%s", c.bin, err, out)
		}
	}
}
