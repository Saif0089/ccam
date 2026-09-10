package editors

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccam/internal/accounts"
)

// envEntry is one {name, value} pair in the extension's setting.
type envEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ReadStoreDir returns the credential store an editor is currently pointed at,
// or "" if ccam has not configured it.
func ReadStoreDir(settingsPath, varName string) string {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}
	value, ok := findValue(string(raw), EnvSetting)
	if !ok {
		return ""
	}
	var entries []envEntry
	if err := json.Unmarshal([]byte(value), &entries); err != nil {
		return ""
	}
	for _, e := range entries {
		if e.Name == varName {
			return e.Value
		}
	}
	return ""
}

// PointAt makes the editor launch Claude with varName set to storeDir, leaving
// every other setting — and every comment, and the file's formatting —
// untouched. Any other variables the user has set there are preserved; only
// ccam's own entry is replaced.
func PointAt(settingsPath, varName, storeDir string) error {
	raw, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(raw)

	entries := []envEntry{}
	if value, ok := findValue(text, EnvSetting); ok {
		// Keep whatever else is in there; drop any earlier ccam entry.
		var existing []envEntry
		if err := json.Unmarshal([]byte(value), &existing); err == nil {
			for _, e := range existing {
				if e.Name != varName {
					entries = append(entries, e)
				}
			}
		}
	}
	entries = append(entries, envEntry{Name: varName, Value: storeDir})

	encoded, err := json.MarshalIndent(entries, "  ", "  ")
	if err != nil {
		return err
	}
	updated, err := upsert(text, EnvSetting, string(encoded))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, []byte(updated), 0o644)
}

// findValue returns the raw JSON text of a top-level key's value.
func findValue(text, key string) (string, bool) {
	start, end, ok := valueSpan(text, key)
	if !ok {
		return "", false
	}
	return text[start:end], true
}

// valueSpan locates a top-level key's value in a settings file, returning the
// byte range of the value itself. It scans rather than parses because
// settings.json is JSON with comments and trailing commas — a real parse would
// mean re-serialising the file and throwing the user's comments away.
func valueSpan(text, key string) (start, end int, ok bool) {
	needle := `"` + key + `"`
	i := indexOutsideString(text, needle)
	if i < 0 {
		return 0, 0, false
	}
	j := i + len(needle)
	for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
		j++
	}
	if j >= len(text) || text[j] != ':' {
		return 0, 0, false
	}
	j++
	for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
		j++
	}
	if j >= len(text) {
		return 0, 0, false
	}
	switch text[j] {
	case '[', '{':
		closing, err := matchBracket(text, j)
		if err != nil {
			return 0, 0, false
		}
		return j, closing + 1, true
	case '"':
		k := j + 1
		for k < len(text) {
			if text[k] == '\\' {
				k += 2
				continue
			}
			if text[k] == '"' {
				return j, k + 1, true
			}
			k++
		}
		return 0, 0, false
	default:
		k := j
		for k < len(text) && text[k] != ',' && text[k] != '\n' && text[k] != '}' {
			k++
		}
		return j, k, true
	}
}

// matchBracket returns the index of the bracket closing the one at open,
// skipping over anything inside strings.
func matchBracket(text string, open int) (int, error) {
	pairs := map[byte]byte{'[': ']', '{': '}'}
	closer, isOpen := pairs[text[open]]
	if !isOpen {
		return 0, fmt.Errorf("not a bracket")
	}
	depth := 0
	inString := false
	for i := open; i < len(text); i++ {
		c := text[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case text[open]:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced %c", text[open])
}

// indexOutsideString finds needle, ignoring occurrences inside a string value
// or a comment, so a key named in a comment is not mistaken for the setting.
func indexOutsideString(text, needle string) int {
	inString, inLine, inBlock := false, false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
			}
		case inBlock:
			if c == '*' && i+1 < len(text) && text[i+1] == '/' {
				inBlock = false
				i++
			}
		case inString:
			if c == '\\' {
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			inLine = true
			i++
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			inBlock = true
			i++
		case c == '"':
			if strings.HasPrefix(text[i:], needle) {
				return i
			}
			inString = true
		}
	}
	return -1
}

// upsert replaces a top-level key's value, or adds the key when it is absent.
func upsert(text, key, value string) (string, error) {
	if start, end, ok := valueSpan(text, key); ok {
		return text[:start] + value + text[end:], nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "{\n  \"" + key + "\": " + value + "\n}\n", nil
	}
	open := strings.Index(text, "{")
	if open < 0 {
		return "", fmt.Errorf("%s is not a JSON object", key)
	}
	rest := strings.TrimLeft(text[open+1:], " \t\r\n")
	sep := ",\n"
	if strings.HasPrefix(rest, "}") {
		sep = "\n" // the object was empty
	}
	return text[:open+1] + "\n  \"" + key + "\": " + value + sep + text[open+1:], nil
}

// PointAtWrapper makes the editor launch Claude through ccam, so each
// conversation gets its own credential store.
//
// It also takes away the entry the older, per-editor scheme left in
// claudeCode.environmentVariables. The two cannot coexist: the extension
// applies that setting LAST, over the environment ccam's wrapper just built, so
// a leftover entry silently puts every conversation back on one shared store
// and per-conversation switching stops working with nothing to show for it.
func PointAtWrapper(settingsPath, ccamBinary string) error {
	raw, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	encoded, err := json.Marshal(ccamBinary)
	if err != nil {
		return err
	}
	updated, err := upsert(string(raw), WrapperSetting, string(encoded))
	if err != nil {
		return err
	}
	updated, err = withoutEnvVar(updated, accounts.SecureStorageEnvVar)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, []byte(updated), 0o644)
}

// withoutEnvVar removes one variable from claudeCode.environmentVariables,
// keeping every other variable the user put there. A setting that ends up empty
// is written as an empty list rather than deleted: the key is the user's, and
// rewriting the file to remove a line is more than is being asked for.
func withoutEnvVar(text, varName string) (string, error) {
	value, ok := findValue(text, EnvSetting)
	if !ok {
		return text, nil
	}
	var existing []envEntry
	if err := json.Unmarshal([]byte(value), &existing); err != nil {
		return text, nil // not ours to interpret; leave it exactly as it is
	}
	kept := []envEntry{}
	for _, e := range existing {
		if e.Name != varName {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(existing) {
		return text, nil
	}
	encoded, err := json.MarshalIndent(kept, "  ", "  ")
	if err != nil {
		return text, err
	}
	return upsert(text, EnvSetting, string(encoded))
}

// WrapperPath is the executable this editor launches Claude through, or "".
func WrapperPath(settingsPath string) string {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}
	value, ok := findValue(string(raw), WrapperSetting)
	if !ok {
		return ""
	}
	var path string
	if err := json.Unmarshal([]byte(value), &path); err != nil {
		return ""
	}
	return path
}

// UnsetWrapper takes ccam back out of an editor's launch path, leaving the
// Claude Code extension to run its own binary exactly as it did before ccam.
//
// Uninstalling has to do this. The setting names an executable by absolute
// path, so a ccam that has been removed leaves the extension launching a file
// that is not there: every conversation fails with "Claude Code process exited
// with code 1", and the thing that could explain why is gone. Removing the key
// rather than blanking it also gives the extension its own update check back,
// which it skips while a wrapper is configured.
//
// The key is deleted with its whitespace and one trailing comma, so the file
// reads as though it had never been there. Everything else — comments,
// formatting, the user's other settings — is untouched.
func UnsetWrapper(settingsPath string) error {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	updated, changed := withoutKey(string(raw), WrapperSetting)
	if !changed {
		return nil
	}
	return os.WriteFile(settingsPath, []byte(updated), 0o644)
}

// withoutKey removes one top-level key and its value from JSONC text.
func withoutKey(text, key string) (string, bool) {
	start, end, ok := valueSpan(text, key)
	if !ok {
		return text, false
	}
	// valueSpan points at the value; walk back over `"key" :` to the quote.
	keyStart := strings.LastIndex(text[:start], `"`+key+`"`)
	if keyStart < 0 {
		return text, false
	}
	// Take the blank line the key sat on with it, but never text before a
	// newline: a comment or another setting on the line above stays put.
	for keyStart > 0 && (text[keyStart-1] == ' ' || text[keyStart-1] == '\t') {
		keyStart--
	}
	// One trailing comma belongs to this entry, not the next one.
	for end < len(text) && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	if end < len(text) && text[end] == ',' {
		end++
	}
	for end < len(text) && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	if end < len(text) && text[end] == '\n' {
		end++
	}
	return text[:keyStart] + text[end:], true
}
