package switching

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"ccam/internal/accounts"
)

func TestParseTrigger(t *testing.T) {
	for _, tc := range []struct {
		prompt   string
		wantName string
		wantOK   bool
	}{
		{"ccam ehti", "ehti", true},
		{"  ccam   ehti  ", "ehti", true},
		{"ccam switch ehti", "ehti", true},
		{"ccam claude-ehti", "claude-ehti", true},
		{"/ccam ehti", "", false},           // slash never reaches the hook as this shape
		{"ccam", "", false},                 // no name
		{"ccam ehti now", "", false},        // extra words → a real prompt
		{"please run ccam ehti", "", false}, // sentence
		{"what does ccam do", "", false},
		{"", "", false},
	} {
		name, ok := ParseTrigger(tc.prompt)
		if ok != tc.wantOK || name != tc.wantName {
			t.Errorf("ParseTrigger(%q) = (%q,%v), want (%q,%v)", tc.prompt, name, ok, tc.wantName, tc.wantOK)
		}
	}
}

func TestResolveAccount(t *testing.T) {
	list := []accounts.Account{
		{ID: "ehti", Slug: "ehti", Alias: "claude-ehti"},
		{ID: "default", Slug: "default", Alias: "claude", Name: "saif@devhouse.co"},
	}
	for _, tc := range []struct{ name, wantID string }{
		{"ehti", "ehti"},
		{"EHTI", "ehti"},          // case-insensitive
		{"claude-ehti", "ehti"},   // full alias
		{"default", "default"},    // slug
		{"claude", "default"},     // alias
	} {
		got, ok := ResolveAccount(list, tc.name)
		if !ok || got.ID != tc.wantID {
			t.Errorf("ResolveAccount(%q) = (%q,%v), want %q", tc.name, got.ID, ok, tc.wantID)
		}
	}
	if _, ok := ResolveAccount(list, "nope"); ok {
		t.Error("unknown name should not resolve")
	}
}

func TestHandoffRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.json")

	if _, ok := ReadHandoff(path); ok {
		t.Error("no handoff should exist yet")
	}
	if err := WriteHandoff(path, Handoff{Account: "ehti", SessionID: "s-1"}); err != nil {
		t.Fatal(err)
	}
	h, ok := ReadHandoff(path)
	if !ok || h.Account != "ehti" || h.SessionID != "s-1" {
		t.Fatalf("round trip failed: %+v ok=%v", h, ok)
	}
	ClearHandoff(path)
	if _, ok := ReadHandoff(path); ok {
		t.Error("handoff should be gone after ClearHandoff")
	}
	ClearHandoff(path) // clearing a missing file is not an error
}

func TestBlockDecisionJSON(t *testing.T) {
	data, err := BlockDecisionJSON("Switching to ehti…")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["decision"] != "block" {
		t.Errorf("decision = %v, want block", m["decision"])
	}
	hso, ok := m["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("missing hookSpecificOutput")
	}
	if hso["hookEventName"] != "UserPromptSubmit" || hso["suppressOriginalPrompt"] != true {
		t.Errorf("hookSpecificOutput wrong: %v", hso)
	}
}
