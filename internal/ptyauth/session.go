// Package ptyauth drives `claude`'s login flow for one account and
// reports it back as a small sequence of events: the OAuth URL to open,
// then either success or failure. It's the only package that has to
// reason about `claude`'s terminal UI at all — everything else (the
// HTTP layer, the account store) only sees Events.
package ptyauth

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
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
	// EventFailed means the login ended without succeeding.
	EventFailed EventType = "failed"
	// EventTimeout means no login was observed within Config.Timeout.
	EventTimeout EventType = "timeout"
)

// Event is one step of an in-progress login.
//
// The json tags matter: these go over the wire to the browser as SSE
// frames, and the web UI reads `type`/`url`/`message`. Without tags Go
// would emit `Type`/`URL`/`Message` and every field would read back as
// undefined in JS — see TestEventJSONWireFormat, which pins the exact
// bytes.
type Event struct {
	Type    EventType `json:"type"`
	URL     string    `json:"url"`
	Message string    `json:"message"`
}

// loginArgs is how ccam asks `claude` to log in.
//
// Not a bare `claude`: on a fresh CLAUDE_CONFIG_DIR that opens the
// interactive first-run flow (a theme picker, then a login-method
// picker) and waits for keystrokes, so nothing ever prints a URL and
// the login just hangs. `auth login --claudeai` goes straight to the
// OAuth step and prints the URL on its own.
var loginArgs = []string{"auth", "login", "--claudeai"}

// urlPattern matches the OAuth URL anywhere in the rendered terminal
// screen — deliberately loose, since `claude`'s exact wording around
// the URL isn't a stable contract to depend on.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

// Config configures one login attempt.
type Config struct {
	// ClaudeBinary is the executable to spawn, normally resolved via
	// claudebin.Resolve. Overridable in tests (fakeclaude).
	ClaudeBinary string
	// ConfigDir becomes the child's CLAUDE_CONFIG_DIR, isolating this
	// login to one account.
	ConfigDir string
	// Env is the child's complete environment (normally os.Environ()
	// plus CLAUDE_CONFIG_DIR).
	Env []string
	// Timeout bounds how long to wait for a login to complete after the
	// URL is shown, in case the user never finishes it.
	Timeout time.Duration
	// PollInterval controls how often the completion probe runs.
	PollInterval time.Duration
	// Prober reports whether ConfigDir has become authenticated. Left
	// nil, a default one is built around ClaudeBinary.
	Prober *accounts.Prober
	// WorkingDir is the directory `claude` runs in. Left empty it
	// defaults to the user's home — never inherit ccam's own working
	// directory, which is "/" when launchd started it at login.
	WorkingDir string
}

// homeDir is the default working directory for spawned claude
// processes.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// Session is one in-flight login attempt.
type Session struct {
	events chan Event
	pty    ptyio.Session
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
}

// Start spawns `claude auth login` for cfg.ConfigDir and begins
// streaming Events. The caller must read Events() until it closes, and
// should call Close when abandoning the attempt so the child process
// doesn't outlive it.
func Start(ctx context.Context, cfg Config) (*Session, error) {
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
		poll = time.Second
	}
	prober := cfg.Prober
	if prober == nil {
		prober = &accounts.Prober{ClaudeBinary: binary}
	}

	workingDir := cfg.WorkingDir
	if workingDir == "" {
		workingDir = homeDir()
	}

	pty, err := ptyio.Start(binary, loginArgs, cfg.Env, workingDir)
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", binary, err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	s := &Session{
		events: make(chan Event, 8),
		pty:    pty,
		cancel: cancel,
	}
	go s.run(ctx, cfg.ConfigDir, prober, poll)
	return s, nil
}

// Events is the session's event stream. It is closed once the attempt
// reaches a terminal state.
func (s *Session) Events() <-chan Event { return s.events }

// SubmitCode types an authorization code into the waiting `claude`
// process, for the flow where the browser hands the user a code to
// paste back ("Paste code here if prompted >") rather than redirecting
// to a local callback.
func (s *Session) SubmitCode(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("code must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("login session has already ended")
	}
	if _, err := s.pty.Write([]byte(code + "\r")); err != nil {
		return fmt.Errorf("sending code to claude: %w", err)
	}
	return nil
}

// Close ends the attempt and the child process.
func (s *Session) Close() {
	s.cancel()
}

func (s *Session) run(ctx context.Context, configDir string, prober *accounts.Prober, poll time.Duration) {
	defer close(s.events)
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.pty.Close()
	}()

	term := vt10x.New(vt10x.WithSize(ptyio.DefaultCols, ptyio.DefaultRows))
	screens := make(chan string, 8)
	go pumpScreen(s.pty, term, screens)

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	urlSent := false
	emit := func(ev Event) {
		select {
		case s.events <- ev:
		case <-ctx.Done():
		}
	}

	for {
		select {
		case text, ok := <-screens:
			if !ok {
				// claude's output ended. It may have just finished a
				// successful login, so give the probe the last word.
				if prober.IsLinked(ctx, configDir) {
					emit(Event{Type: EventLinked})
				} else {
					emit(Event{Type: EventFailed, Message: "claude exited before completing login"})
				}
				return
			}
			if !urlSent {
				if url := urlPattern.FindString(text); url != "" {
					urlSent = true
					emit(Event{Type: EventURL, URL: url})
				}
			}
		case <-ticker.C:
			if prober.IsLinked(ctx, configDir) {
				emit(Event{Type: EventLinked})
				return
			}
		case <-ctx.Done():
			if prober.IsLinked(context.Background(), configDir) {
				emit(Event{Type: EventLinked})
			} else {
				emit(Event{Type: EventTimeout, Message: "timed out waiting for login to complete"})
			}
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
