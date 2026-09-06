package httpserver

import (
	"sync"

	"ccam/internal/ptyauth"
)

// loginBroadcast fans one account's login events out to every SSE
// connection watching it, replaying everything seen so far to a new
// subscriber (covering the common case of POST /login starting the
// flow slightly before the browser's SSE GET attaches) and to a
// reconnect after a network blip.
type loginBroadcast struct {
	session *ptyauth.Session

	mu          sync.Mutex
	history     []ptyauth.Event
	done        bool
	subscribers map[chan ptyauth.Event]struct{}
}

func newLoginBroadcast(session *ptyauth.Session) *loginBroadcast {
	return &loginBroadcast{
		session:     session,
		subscribers: map[chan ptyauth.Event]struct{}{},
	}
}

// run drains events into the broadcast until the channel closes, then
// marks the login done and closes every subscriber.
func (b *loginBroadcast) run(events <-chan ptyauth.Event) {
	for ev := range events {
		b.publish(ev)
	}
	b.mu.Lock()
	b.done = true
	for ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = map[chan ptyauth.Event]struct{}{}
	b.mu.Unlock()
}

func (b *loginBroadcast) publish(ev ptyauth.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history = append(b.history, ev)
	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default: // a slow/gone subscriber never blocks the login itself
		}
	}
}

// subscribe returns a channel replaying history so far, followed by
// live events, and an unsubscribe func the caller must defer. If the
// login has already finished, the returned channel is pre-closed after
// replaying history (never blocks waiting for more).
func (b *loginBroadcast) subscribe() (<-chan ptyauth.Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan ptyauth.Event, len(b.history)+8)
	for _, ev := range b.history {
		ch <- ev
	}
	if b.done {
		close(ch)
		return ch, func() {}
	}
	b.subscribers[ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		b.mu.Unlock()
	}
}

// finished reports whether the login has reached a terminal state.
func (b *loginBroadcast) finished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done
}

// stop ends the underlying claude process.
func (b *loginBroadcast) stop() {
	if b.session != nil {
		b.session.Close()
	}
}

// submitCode types an authorization code into the waiting claude.
func (b *loginBroadcast) submitCode(code string) error {
	return b.session.SubmitCode(code)
}
