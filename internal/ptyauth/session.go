// Package ptyauth drives `claude`'s interactive login flow for one
// account and reports it back as a small sequence of events: the OAuth
// URL to open, then either success or failure. It's the only package
// that has to reason about `claude`'s TUI at all — everything else
// (the HTTP layer, the account store) only sees Events.
package ptyauth

import (
	"bufio"
	"context"
	"regexp"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/ptyio"

	"github.com/hinshun/vt10x"
)

// EventType identifies what kind of Event was emitted.
type EventType string

const (
	// EventURL carries the OAuth URL the user must open in a browser.
	EventURL EventType = "url"
	// EventLinked means the login was observed to succeed.
	EventLinked EventType = "linked"
	// EventFailed means the child process exited without a successful
	// login being observed.
	EventFailed EventType = "failed"
	// EventTimeout means no login was observed within Session's timeout.
	EventTimeout EventType = "timeout"
)

// Event is one step of an in-progress login.
type Event struct {
	Type    EventType
	URL     string
	Message string
}

// urlPattern matches the first http(s) URL appearing anywhere in the
// rendered terminal screen — deliberately loose, since `claude`'s exact
// wording around the URL isn't a stable contract to depend on.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// Config configures one login attempt.
type Config struct {
	// ClaudeBinary is the executable to spawn, normally "claude" via
	// PATH. Overridable in tests (fakeclaude).
	ClaudeBinary string
	// ConfigDir becomes the child's CLAUDE_CONFIG_DIR, isolating this
	// login to one account.
	ConfigDir string
	// ExtraEnv is appended to the child's environment (normally
	// os.Environ() plus CLAUDE_CONFIG_DIR).
	Env []string
	// Timeout bounds how long to wait for a login to complete after the
	// URL is shown, in case the user never finishes it.
	Timeout time.Duration
	// PollInterval controls how often the completion probe runs.
	PollInterval time.Duration
}

// Run spawns `claude`'s login flow per cfg and streams Events to the
// returned channel, which is closed once the attempt reaches a terminal
// state (linked, failed, or timeout) or ctx is canceled. The caller
// should read until the channel closes.
func Run(ctx context.Context, cfg Config) <-chan Event {
	events := make(chan Event, 8)
	go run(ctx, cfg, events)
	return events
}

func run(ctx context.Context, cfg Config, events chan<- Event) {
	defer close(events)

	binary := cfg.ClaudeBinary
	if binary == "" {
		binary = "claude"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}

	sess, err := ptyio.Start(binary, nil, cfg.Env)
	if err != nil {
		events <- Event{Type: EventFailed, Message: "starting claude: " + err.Error()}
		return
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	term := vt10x.New(vt10x.WithSize(120, 40))
	screenText := make(chan string, 8)
	go pumpScreen(sess, term, screenText)

	prober := &accounts.Prober{ClaudeBinary: binary, Timeout: 10 * time.Second}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	urlSent := false
	for {
		select {
		case text, ok := <-screenText:
			if !ok {
				// The child's output stream closed. Give the completion
				// probe one last chance (it may have exited right after
				// writing credentials) before declaring failure.
				if prober.IsLinked(ctx, cfg.ConfigDir) {
					events <- Event{Type: EventLinked}
				} else {
					events <- Event{Type: EventFailed, Message: "claude exited before completing login"}
				}
				return
			}
			if !urlSent {
				if url := urlPattern.FindString(text); url != "" {
					urlSent = true
					events <- Event{Type: EventURL, URL: url}
				}
			}
		case <-ticker.C:
			if prober.IsLinked(ctx, cfg.ConfigDir) {
				events <- Event{Type: EventLinked}
				return
			}
		case <-ctx.Done():
			events <- Event{Type: EventTimeout, Message: "timed out waiting for login"}
			return
		}
	}
}

// pumpScreen continuously parses sess's output into term and pushes the
// rendered screen text to out after each parse, until sess's stream
// ends. It uses the rendered screen (not raw bytes) because `claude`'s
// TUI repaints/clears lines rather than printing plain scrolling text.
func pumpScreen(sess ptyio.Session, term vt10x.Terminal, out chan<- string) {
	defer close(out)
	reader := bufio.NewReader(sess)
	for {
		if err := term.Parse(reader); err != nil {
			return
		}
		out <- term.String()
	}
}
