package ptyauth

import (
	"encoding/json"
	"testing"
)

// TestEventJSONWireFormat pins the exact bytes the browser receives.
//
// This test exists because of a real bug: Event had no json tags, so
// SSE frames carried {"Type":...,"URL":...} while the web UI read
// event.type / event.url — every field came back undefined, the OAuth
// URL was never displayed, and the login could not be completed. Both
// the Go e2e test and the httpserver test decoded into the Go struct
// itself, so they round-tripped Go↔Go and rubber-stamped the broken
// contract. Asserting the literal JSON is the only way to catch it.
func TestEventJSONWireFormat(t *testing.T) {
	data, err := json.Marshal(Event{Type: EventURL, URL: "https://example.com/auth"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	const want = `{"type":"url","url":"https://example.com/auth","message":""}`
	if string(data) != want {
		t.Errorf("SSE frame = %s\nwant %s", data, want)
	}
}

func TestEventTypesMatchWebUIStrings(t *testing.T) {
	// The web UI switches on these literal strings; renaming a constant
	// without updating app.js would silently drop the event.
	for _, want := range []struct {
		got  EventType
		text string
	}{
		{EventURL, "url"},
		{EventLinked, "linked"},
		{EventFailed, "failed"},
		{EventTimeout, "timeout"},
	} {
		if string(want.got) != want.text {
			t.Errorf("event type = %q, want %q", want.got, want.text)
		}
	}
}
