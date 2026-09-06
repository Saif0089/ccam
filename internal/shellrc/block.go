// Package shellrc maintains one idempotent, clearly delimited block of
// generated content inside a user's shell rc file (their aliases,
// prompt, plugins, everything else) without ever touching a single byte
// outside that block.
package shellrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "# >>> ccam accounts >>>"
	endMarker   = "# <<< ccam accounts <<<"
)

// UpsertBlock ensures path contains exactly one managed block with the
// given body (a multi-line string, no markers, no surrounding blank
// lines needed). If path doesn't exist yet, it's created. If a managed
// block already exists, it's replaced in place; otherwise the block is
// appended, separated from any existing content by one blank line.
func UpsertBlock(path, body string) error {
	original, existed, err := readFile(path)
	if err != nil {
		return err
	}

	block := beginMarker + "\n" + strings.Trim(body, "\n") + "\n" + endMarker + "\n"

	start, end := findBlock(original)
	var updated string
	if start >= 0 {
		updated = original[:start] + block + original[end:]
	} else {
		updated = appendBlock(original, block)
	}

	if updated == original && existed {
		return nil // no-op write avoided; also keeps mtime stable for tests
	}
	return writeFile(path, updated, existed)
}

// RemoveBlock deletes the managed block from path, if present, leaving
// everything else byte-for-byte unchanged. A missing file, or a file
// with no managed block, is not an error.
func RemoveBlock(path string) error {
	original, existed, err := readFile(path)
	if err != nil {
		return err
	}
	if !existed {
		return nil
	}

	start, end := findBlock(original)
	if start < 0 {
		return nil
	}

	updated := original[:start] + original[end:]
	// If we're removing a block that UpsertBlock appended after existing
	// content, undo the one separating blank line it added too.
	if start >= 2 && original[start-1] == '\n' && original[start-2] == '\n' {
		updated = original[:start-1] + original[end:]
	}

	if updated == "" {
		// The file held nothing but our block (the common case for an rc
		// file ccam itself created) — remove it entirely rather than
		// leaving a stray empty file behind.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	return writeFile(path, updated, existed)
}

// HasBlock reports whether path currently contains a managed block.
func HasBlock(path string) (bool, error) {
	original, existed, err := readFile(path)
	if err != nil || !existed {
		return false, err
	}
	start, _ := findBlock(original)
	return start >= 0, nil
}

// findBlock returns the [start, end) byte range of the managed block,
// including its markers and trailing newline, or (-1, -1) if absent.
func findBlock(content string) (int, int) {
	start := strings.Index(content, beginMarker)
	if start < 0 {
		return -1, -1
	}
	endIdx := strings.Index(content[start:], endMarker)
	if endIdx < 0 {
		return -1, -1
	}
	end := start + endIdx + len(endMarker)
	// Consume the newline right after the end marker, if present, so the
	// block "owns" its own trailing line break.
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return start, end
}

func appendBlock(original, block string) string {
	if original == "" {
		return block
	}
	if strings.HasSuffix(original, "\n") {
		return original + "\n" + block
	}
	return original + "\n\n" + block
}

func readFile(path string) (content string, existed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

func writeFile(path, content string, existed bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	mode := os.FileMode(0o644)
	if existed {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode()
		}
	}
	tmp := path + ".ccam-tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
