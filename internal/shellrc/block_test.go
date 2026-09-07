package shellrc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertBlockOnMissingFileCreatesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")

	if err := UpsertBlock(path, "alias claude-work='CLAUDE_CONFIG_DIR=/x claude'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := beginMarker + "\n" + "alias claude-work='CLAUDE_CONFIG_DIR=/x claude'\n" + endMarker + "\n"
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestUpsertBlockAppendsAfterExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	original := "export PATH=\"$HOME/bin:$PATH\"\nalias ll='ls -la'\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := UpsertBlock(path, "alias claude-work='CLAUDE_CONFIG_DIR=/x claude'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}

	got, _ := os.ReadFile(path)
	want := original + "\n" + beginMarker + "\n" + "alias claude-work='CLAUDE_CONFIG_DIR=/x claude'\n" + endMarker + "\n"
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestUpsertBlockIsIdempotentAndReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	original := "export PATH=\"$HOME/bin:$PATH\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := UpsertBlock(path, "alias a='one'\n"); err != nil {
		t.Fatalf("UpsertBlock 1: %v", err)
	}
	if err := UpsertBlock(path, "alias a='one'\nalias b='two'\n"); err != nil {
		t.Fatalf("UpsertBlock 2: %v", err)
	}

	got, _ := os.ReadFile(path)
	want := original + "\n" + beginMarker + "\n" + "alias a='one'\nalias b='two'\n" + endMarker + "\n"
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}

	// Running the exact same upsert again must change nothing further.
	before, _ := os.Stat(path)
	if err := UpsertBlock(path, "alias a='one'\nalias b='two'\n"); err != nil {
		t.Fatalf("UpsertBlock 3: %v", err)
	}
	after, _ := os.Stat(path)
	if before.ModTime() != after.ModTime() {
		t.Error("expected a no-op re-upsert to leave mtime unchanged")
	}
}

func TestRemoveBlockRestoresOriginalByteForByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	original := "export PATH=\"$HOME/bin:$PATH\"\nalias ll='ls -la'\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := UpsertBlock(path, "alias claude-work='...'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}
	if err := RemoveBlock(path); err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != original {
		t.Errorf("content after remove = %q, want original %q", got, original)
	}
}

func TestRemoveBlockOnFileWithOnlyTheBlockDeletesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")

	if err := UpsertBlock(path, "alias claude-work='...'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}
	if err := RemoveBlock(path); err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err = %v", err)
	}
}

func TestRemoveBlockOnMissingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if err := RemoveBlock(path); err != nil {
		t.Errorf("RemoveBlock on missing file: %v", err)
	}
}

func TestRemoveBlockOnFileWithoutBlockIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")
	original := "export PATH=\"$HOME/bin:$PATH\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := RemoveBlock(path); err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("content = %q, want unchanged %q", got, original)
	}
}

func TestHasBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".zshrc")

	if has, err := HasBlock(path); err != nil || has {
		t.Fatalf("HasBlock on missing file = %v, %v; want false, nil", has, err)
	}

	if err := UpsertBlock(path, "alias a='one'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}
	if has, err := HasBlock(path); err != nil || !has {
		t.Fatalf("HasBlock after upsert = %v, %v; want true, nil", has, err)
	}

	if err := RemoveBlock(path); err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}
	if has, err := HasBlock(path); err != nil || has {
		t.Fatalf("HasBlock after remove = %v, %v; want false, nil", has, err)
	}
}

// TestUpsertBlockWritesThroughASymlink covers dotfile managers
// (chezmoi, stow, yadm), which symlink ~/.zshrc into a repo. Replacing
// that link with a regular file is worse than it sounds: the repo copy
// silently stops reaching the shell, and every later `chezmoi apply`
// appears to work while changing nothing.
func TestUpsertBlockWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-zshrc")
	link := filepath.Join(dir, ".zshrc")

	if err := os.WriteFile(target, []byte("# from my dotfiles repo\n"), 0o644); err != nil {
		t.Fatalf("seeding target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	if err := UpsertBlock(link, "alias claude-work='...'\n"); err != nil {
		t.Fatalf("UpsertBlock: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}

	// The content must have landed in the repo copy, where the dotfile
	// manager will see it.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading target: %v", err)
	}
	if !contains(string(data), "claude-work") {
		t.Errorf("alias did not reach the symlink target:\n%s", data)
	}
	if !contains(string(data), "# from my dotfiles repo") {
		t.Error("existing content was lost")
	}

	if err := RemoveBlock(link); err != nil {
		t.Fatalf("RemoveBlock: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink did not survive removal")
	}
	data, _ = os.ReadFile(target)
	if string(data) != "# from my dotfiles repo\n" {
		t.Errorf("target not restored:\n%s", data)
	}
}
