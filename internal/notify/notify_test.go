package notify

import (
	"runtime"
	"strings"
	"testing"
)

// The body is user-visible text that ends up inside a shell-free but
// still quoted context — an AppleScript literal, a PowerShell literal.
// A release name with a quote in it must not be able to end that
// literal and turn the rest into something else to run.
func TestNotifyCommandQuotesTheBody(t *testing.T) {
	body := `updated to "v1.0" '; echo pwned; #`

	name, args, err := notifyCommand(body)
	if err != nil {
		// Linux without notify-send installed: nothing to quote, and
		// the caller is told there is nowhere to show it.
		if runtime.GOOS == "linux" {
			t.Skip("no notify-send on this machine")
		}
		t.Fatalf("notifyCommand: %v", err)
	}
	if name == "" {
		t.Fatal("no command to run")
	}

	joined := strings.Join(args, " ")
	switch runtime.GOOS {
	case "darwin":
		// AppleScript escapes a quote as \" — an unescaped one would
		// close the string.
		if strings.Contains(joined, `"v1.0"`) {
			t.Errorf("quotes left unescaped in: %s", joined)
		}
		if !strings.Contains(joined, `\"v1.0\"`) {
			t.Errorf("want the quotes escaped for AppleScript, got: %s", joined)
		}
	case "windows":
		// PowerShell single-quoted literals escape ' by doubling it.
		if !strings.Contains(joined, `'''; echo pwned; #'`) {
			t.Errorf("want the apostrophe doubled for PowerShell, got: %s", joined)
		}
	case "linux":
		// notify-send takes the body as one argv entry, so nothing
		// needs escaping — but it does have to be there, whole.
		if args[len(args)-1] != body {
			t.Errorf("body = %q, want it passed through as one argument", args[len(args)-1])
		}
	}
}

func TestNotifyCommandNamesCcam(t *testing.T) {
	_, args, err := notifyCommand("hello")
	if err != nil {
		t.Skipf("no notification mechanism here: %v", err)
	}
	if !strings.Contains(strings.Join(args, " "), title) {
		t.Errorf("want %q in the notification, got: %s", title, strings.Join(args, " "))
	}
}
