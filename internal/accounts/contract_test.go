package accounts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// whyThisTestExists is printed with every failure below, because the
// developer who breaks this contract will be renaming a struct field in
// model.go, nowhere near this file, and nothing else will complain.
const whyThisTestExists = `
WHY THIS TEST EXISTS: ~/.ccam/accounts.json is not private to ccam. External
tools read it to discover every Claude account on the machine — the Claude
usage monitor (~/Documents/GitHub/claude-usage-monitor) parses this exact
file to attribute usage per account, keying off "slug" and reading each
account's own .claude.json and projects/ transcripts under "configDir".
Renaming or dropping one of these JSON field names compiles, passes every
other test, and silently breaks usage attribution on every machine — no
error, just wrong numbers. If you need to change the shape here, change
those readers too and update docs/ARCHITECTURE.md.`

// documentedAccountFields is the published key set of one account object.
// Adding a key is compatible (readers ignore what they don't know), so a
// new key is reported below but does not fail; removing or renaming one is
// what breaks readers.
var documentedAccountFields = []string{
	"id", "name", "slug", "kind", "configDir", "alias", "status", "createdAt", "lastUsedAt",
}

// zeroTimeJSON is what an account that has never been used writes for
// "lastUsedAt". The struct tag says `omitempty`, but Go does not treat a
// zero time.Time as empty, so the key is always there — and readers parse
// it as a date. Dropping it (a pointer, a custom marshaller) would hand
// them a null they don't expect.
const zeroTimeJSON = `"0001-01-01T00:00:00Z"`

// newContractStore builds a manager over a temp dir and hands back the
// path of the file it writes, so the assertions can read the real bytes
// the store produced rather than re-marshalling a struct — the point is to
// catch a change in how ccam writes, which a hand-built struct would miss.
func newContractStore(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	return NewManager(NewStore(path), filepath.Join(dir, "accounts")), path
}

// readAccountsFile returns the account objects as raw JSON maps, exactly
// as an outside reader would see them.
func readAccountsFile(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v%s", path, err, whyThisTestExists)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("accounts.json is not a JSON object: %v\n%s%s", err, data, whyThisTestExists)
	}
	raw, ok := top["accounts"]
	if !ok {
		t.Fatalf("accounts.json has no top-level \"accounts\" array; keys are %v%s", sortedKeys(top), whyThisTestExists)
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("\"accounts\" is not an array of objects: %v%s", err, whyThisTestExists)
	}
	return list
}

// checkDocumentedFields fails on a missing documented key and merely logs
// an unexpected one.
func checkDocumentedFields(t *testing.T, label string, account map[string]json.RawMessage) {
	t.Helper()

	documented := map[string]bool{}
	for _, field := range documentedAccountFields {
		documented[field] = true
		if _, ok := account[field]; !ok {
			t.Errorf("%s account is missing the documented field %q; it has %v%s",
				label, field, sortedKeys(account), whyThisTestExists)
		}
	}
	for _, field := range sortedKeys(account) {
		if !documented[field] {
			t.Logf("note: %s account carries the undocumented field %q. Adding a field is "+
				"compatible, but external readers won't see it until docs/ARCHITECTURE.md and "+
				"documentedAccountFields in this test say it exists.", label, field)
		}
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringField(t *testing.T, account map[string]json.RawMessage, field string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(account[field], &value); err != nil {
		t.Fatalf("%q is not a JSON string: %v%s", field, err, whyThisTestExists)
	}
	return value
}

// TestAccountsJSONKeepsTheFieldNamesExternalUsageToolsRead is the guard on
// the file's shape: an account written by the real Add/SetStatus path must
// still expose every documented key, and its configDir must still be the
// absolute directory a reader is expected to open.
func TestAccountsJSONKeepsTheFieldNamesExternalUsageToolsRead(t *testing.T) {
	m, path := newContractStore(t)

	added, err := m.Add("Work")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A never-used account is the weaker of the two states: check it
	// carries every key too, and writes the zero date rather than nothing.
	fresh := readAccountsFile(t, path)
	if len(fresh) != 1 {
		t.Fatalf("accounts array holds %d entries, want 1%s", len(fresh), whyThisTestExists)
	}
	checkDocumentedFields(t, "freshly added managed", fresh[0])
	if got := string(fresh[0]["lastUsedAt"]); got != zeroTimeJSON {
		t.Errorf("lastUsedAt on a never-used account = %s, want %s: readers parse this as a date%s",
			got, zeroTimeJSON, whyThisTestExists)
	}

	// Then the steady state a reader mostly meets: a linked account.
	if _, err := m.SetStatus(added.ID, StatusLinked); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	list := readAccountsFile(t, path)
	if len(list) != 1 {
		t.Fatalf("accounts array holds %d entries, want 1%s", len(list), whyThisTestExists)
	}
	account := list[0]

	checkDocumentedFields(t, "managed", account)
	for _, field := range documentedAccountFields {
		if _, ok := account[field]; !ok {
			t.Fatalf("cannot check values: %q absent%s", field, whyThisTestExists)
		}
	}

	if kind := stringField(t, account, "kind"); kind != string(KindManaged) {
		t.Errorf("kind = %q, want %q%s", kind, KindManaged, whyThisTestExists)
	}
	if slug := stringField(t, account, "slug"); slug != added.Slug {
		t.Errorf("slug = %q, want %q — it is the stable short id readers key on%s", slug, added.Slug, whyThisTestExists)
	}

	// configDir is the promise that matters most: a reader opens it to
	// find that account's .claude.json and projects/ transcripts.
	configDir := stringField(t, account, "configDir")
	if configDir == "" {
		t.Fatalf("configDir is empty for a managed account; empty means \"the default account, no "+
			"CLAUDE_CONFIG_DIR override\" and would make a reader attribute this account's usage to "+
			"the default one%s", whyThisTestExists)
	}
	if !filepath.IsAbs(configDir) {
		t.Errorf("configDir = %q, want an absolute path: a reader has no idea what it would be relative to%s",
			configDir, whyThisTestExists)
	}
	info, err := os.Stat(configDir)
	if err != nil || !info.IsDir() {
		t.Errorf("configDir %q is not an existing directory: %v%s", configDir, err, whyThisTestExists)
	}
}

// TestAccountsJSONWritesTheDefaultAccountWithAnEmptyConfigDir pins the one
// value with a meaning attached: "" is how a reader tells that this row is
// the account plain `claude` uses, whose transcripts live in ~/.claude
// rather than under ~/.ccam/accounts.
func TestAccountsJSONWritesTheDefaultAccountWithAnEmptyConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	signInDefault(t, home)

	m, path := newContractStore(t)
	prober := &Prober{ClaudeBinary: buildFakeClaude(t)}

	adopted, err := m.EnsureDefault(context.Background(), prober)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if !adopted {
		t.Skip("this build cannot adopt a default account here; nothing to check")
	}

	list := readAccountsFile(t, path)
	var account map[string]json.RawMessage
	for _, candidate := range list {
		if stringField(t, candidate, "kind") == string(KindDefault) {
			account = candidate
			break
		}
	}
	if account == nil {
		t.Fatalf("no account with kind %q in the file after adopting the default one%s", KindDefault, whyThisTestExists)
	}

	checkDocumentedFields(t, "default", account)

	raw, ok := account["configDir"]
	if !ok {
		t.Fatalf("the default account has no \"configDir\" key at all; readers test it for emptiness, "+
			"so it must be present and \"\"%s", whyThisTestExists)
	}
	if configDir := stringField(t, account, "configDir"); configDir != "" {
		t.Errorf("default account configDir = %q (raw %s), want \"\": empty is exactly how a reader "+
			"identifies the account with no CLAUDE_CONFIG_DIR override%s", configDir, raw, whyThisTestExists)
	}
	if id := stringField(t, account, "id"); id != DefaultAccountID {
		t.Errorf("default account id = %q, want %q%s", id, DefaultAccountID, whyThisTestExists)
	}
}
