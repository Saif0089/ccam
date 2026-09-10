package switching

import (
	"path/filepath"
	"testing"
	"time"
)

// The supervisor cannot say what it did: Claude Code owns the terminal while it
// runs, so printing corrupts the TUI. It reports here instead, and the hook —
// whose reply Claude Code renders properly — says it.
func TestOutcomeCarriesTheSupervisorsAnswerToTheHook(t *testing.T) {
	handoff := filepath.Join(t.TempDir(), ".handoff-1.json")

	if _, ok := AwaitOutcome(handoff, 10*time.Millisecond, time.Millisecond); ok {
		t.Fatal("reported an outcome before one was written")
	}

	want := Outcome{OK: true, Message: "Switched to ehti. Same session."}
	if err := WriteOutcome(handoff, want); err != nil {
		t.Fatal(err)
	}
	got, ok := AwaitOutcome(handoff, time.Second, time.Millisecond)
	if !ok || got != want {
		t.Fatalf("read back %+v (ok=%v), want %+v", got, ok, want)
	}

	// Reading takes it away: the answer to one switch must never be shown as
	// the answer to the next.
	if _, ok := AwaitOutcome(handoff, 10*time.Millisecond, time.Millisecond); ok {
		t.Error("the same outcome was reported twice")
	}
}

// A hook stages a switch and then waits. Anything left over from an earlier one
// would be read as this one's answer, so staging clears it first.
func TestClearOutcomeRemovesAStaleAnswer(t *testing.T) {
	handoff := filepath.Join(t.TempDir(), ".handoff-2.json")
	if err := WriteOutcome(handoff, Outcome{Message: "an old answer"}); err != nil {
		t.Fatal(err)
	}
	ClearOutcome(handoff)
	if _, ok := AwaitOutcome(handoff, 10*time.Millisecond, time.Millisecond); ok {
		t.Error("a cleared outcome was still readable")
	}
}

// A supervisor that never answers must not hold the prompt up for ever, and
// must not be reported as a result either.
func TestAwaitOutcomeGivesUp(t *testing.T) {
	handoff := filepath.Join(t.TempDir(), ".handoff-3.json")
	start := time.Now()
	if _, ok := AwaitOutcome(handoff, 60*time.Millisecond, 5*time.Millisecond); ok {
		t.Fatal("claimed an outcome that was never written")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %v, far past the timeout", elapsed)
	}
}
