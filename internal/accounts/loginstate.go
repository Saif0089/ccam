package accounts

import (
	"context"
	"sync"
	"time"

	"ccam/internal/claudebin"
)

// loginStateTTL is how long an observed login state is reused. The web UI
// re-reads the account list every few seconds and each probe is a process
// spawn, so the answer has to be cached to be affordable at all.
const loginStateTTL = 30 * time.Second

// probeTimeout bounds one `claude auth status` call. It runs in the
// background, so a hung probe costs nothing but a goroutine — this only
// stops them accumulating.
const probeTimeout = 10 * time.Second

type loginState struct {
	at       time.Time
	linked   bool
	observed bool
	inflight bool
}

var (
	loginMu    sync.Mutex
	loginCache = map[string]loginState{}

	// probeLogin asks Claude Code itself. Replaced in tests.
	probeLogin = func(configDir string) bool {
		p := &Prober{ClaudeBinary: claudebin.Resolve(), Timeout: probeTimeout}
		return p.IsLinked(context.Background(), configDir)
	}
	loginNow = time.Now
)

// loginStillThere reports whether an account's login is really there.
//
// It answers from the last observation and refreshes in the background,
// because the callers are List and Get — both on the path of an HTTP
// request that the page makes every few seconds, and neither can afford to
// wait on a subprocess.
//
// Until something has actually been observed it reports known=false, and
// the caller leaves the stored status alone. Guessing in either direction
// is worse than saying nothing: claiming "linked" from a file that happens
// to exist repeats the bug this replaced, and downgrading an account
// because ccam has not looked yet would make every restart briefly
// report every account as disconnected.
func loginStillThere(configDir string) (linked, known bool) {
	loginMu.Lock()
	defer loginMu.Unlock()

	e := loginCache[configDir]
	stale := !e.observed || loginNow().Sub(e.at) >= loginStateTTL
	if stale && !e.inflight {
		e.inflight = true
		loginCache[configDir] = e
		go refreshLoginState(configDir)
	}
	return e.linked, e.observed
}

func refreshLoginState(configDir string) {
	result := probeLogin(configDir)

	loginMu.Lock()
	defer loginMu.Unlock()
	loginCache[configDir] = loginState{at: loginNow(), linked: result, observed: true}
}
