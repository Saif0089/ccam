package config

import "testing"

func TestRevocationMarkerRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if IsRevoked("work") {
		t.Fatal("nothing marked yet, but IsRevoked is true")
	}
	if err := MarkRevoked("work"); err != nil {
		t.Fatal(err)
	}
	if !IsRevoked("work") {
		t.Fatal("MarkRevoked did not take")
	}
	// A different account is unaffected.
	if IsRevoked("personal") {
		t.Error("marking work also marked personal")
	}
	if err := ClearRevoked("work"); err != nil {
		t.Fatal(err)
	}
	if IsRevoked("work") {
		t.Error("ClearRevoked did not remove the marker")
	}
	// Clearing an absent marker is not an error.
	if err := ClearRevoked("work"); err != nil {
		t.Errorf("clearing an absent marker: %v", err)
	}
}
