package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readClaudeConfig(t *testing.T, configDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("reading .claude.json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parsing .claude.json: %v", err)
	}
	return out
}

// TestMarkOnboardedPreservesExistingConfig is the point of the whole
// file: the config it writes into is Claude Code's own, holding the
// account's oauth identity and project history. Losing any of that to
// set one flag would be far worse than the wizard it prevents.
func TestMarkOnboardedPreservesExistingConfig(t *testing.T) {
	configDir := t.TempDir()
	original := `{"oauthAccount":{"emailAddress":"me@example.com"},"projects":{"/tmp/x":{"allowedTools":[]}},"userID":"abc"}`
	if err := os.WriteFile(filepath.Join(configDir, ".claude.json"), []byte(original), 0o600); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	if err := MarkOnboarded(configDir, buildFakeClaude(t)); err != nil {
		t.Fatalf("MarkOnboarded: %v", err)
	}

	config := readClaudeConfig(t, configDir)
	if config["hasCompletedOnboarding"] != true {
		t.Errorf("hasCompletedOnboarding = %v, want true", config["hasCompletedOnboarding"])
	}
	if config["userID"] != "abc" {
		t.Errorf("userID = %v, want it preserved", config["userID"])
	}
	if _, ok := config["oauthAccount"]; !ok {
		t.Error("oauthAccount was dropped")
	}
	if _, ok := config["projects"]; !ok {
		t.Error("projects were dropped")
	}
}

func TestMarkOnboardedCreatesConfigWhenMissing(t *testing.T) {
	configDir := t.TempDir()

	if err := MarkOnboarded(configDir, buildFakeClaude(t)); err != nil {
		t.Fatalf("MarkOnboarded: %v", err)
	}

	config := readClaudeConfig(t, configDir)
	if config["hasCompletedOnboarding"] != true {
		t.Errorf("hasCompletedOnboarding = %v, want true", config["hasCompletedOnboarding"])
	}
}

func TestMarkOnboardedRecordsVersion(t *testing.T) {
	configDir := t.TempDir()

	if err := MarkOnboarded(configDir, buildFakeClaude(t)); err != nil {
		t.Fatalf("MarkOnboarded: %v", err)
	}

	// Claude Code re-runs onboarding when the recorded version is older
	// than the running one, so the version has to be recorded too.
	config := readClaudeConfig(t, configDir)
	if got, ok := config["lastOnboardingVersion"].(string); !ok || got == "" {
		t.Errorf("lastOnboardingVersion = %v, want a version string", config["lastOnboardingVersion"])
	}
}

func TestMarkOnboardedLeavesUnparseableConfigAlone(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, ".claude.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	if err := MarkOnboarded(configDir, buildFakeClaude(t)); err == nil {
		t.Error("expected an error rather than clobbering a config we can't parse")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{not json" {
		t.Errorf("config was modified: %s", data)
	}
}
