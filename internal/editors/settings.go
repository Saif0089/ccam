package editors

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, []byte(updated), 0o644)
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
