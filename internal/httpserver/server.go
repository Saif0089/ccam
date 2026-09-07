// Package httpserver serves ccam's REST/SSE API and embedded web UI on
// 127.0.0.1 only — this tool manages login credentials, so it must never
// be reachable from anything but the same machine.
package httpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccam/internal/accounts"
	"ccam/internal/config"
	"ccam/internal/service"
	"ccam/internal/shellrc"
	"ccam/internal/termlauncher"
)

// Server holds every dependency the HTTP handlers need.
type Server struct {
	manager      *accounts.Manager
	syncer       *shellrc.Syncer
	claudeBinary string

	// launchTerminal opens a terminal scoped to an account; a field
	// (rather than calling termlauncher.Launch directly) so tests can
	// stub it out without actually opening a window.
	launchTerminal func(configDir, label string) error

	// defaultCheck throttles re-detection of the default account, which
	// costs a `claude auth status` subprocess.
	defaultCheckMu   sync.Mutex
	defaultCheckedAt time.Time

	mu     sync.Mutex
	logins map[string]*loginBroadcast // accountID -> in-progress/last login, if any
	// startMu serialises login starts, which span a subprocess spawn
	// and so can't be done under mu.
	startMu sync.Mutex
}

// New builds a Server. claudeBinary is the executable to spawn for
// logins and probes (normally "claude", overridable for tests).
func New(manager *accounts.Manager, syncer *shellrc.Syncer, claudeBinary string) *Server {
	return &Server{
		manager:        manager,
		syncer:         syncer,
		claudeBinary:   claudeBinary,
		launchTerminal: termlauncher.Launch,
		logins:         map[string]*loginBroadcast{},
	}
}

// Handler returns the complete http.Handler: the embedded web UI plus
// the JSON/SSE API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return withLocalOnly(withLogging(mux))
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// Serve runs the HTTP server on 127.0.0.1:port (0 to pick any free
// port), writes the chosen port to ccam's port file so other ccam
// invocations and the installer's health check can find it, records
// this process's PID, and blocks until ctx is canceled — at which point
// it shuts down gracefully and cleans up both files.
func Serve(ctx context.Context, srv *Server, port int) error {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("listening on 127.0.0.1:%d: %w", port, err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	if err := writePortFile(actualPort); err != nil {
		ln.Close()
		return err
	}
	defer removePortFile(actualPort)

	if err := service.RecordSelf(); err != nil {
		log.Printf("warning: could not record pid file: %v", err)
	}

	// Adopt the account plain `claude` uses, and keep watching for it.
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	srv.StartDefaultAccountWatch(watchCtx)

	httpSrv := &http.Server{Handler: srv.Handler()}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.Serve(ln)
	}()

	log.Printf("ccam listening on http://127.0.0.1:%d", actualPort)

	select {
	case <-ctx.Done():
		// End any in-flight logins first: their `claude` children are
		// in their own session (setsid), so shutting down without this
		// would leave them running with nothing to stop them.
		srv.stopAllLogins()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func writePortFile(port int) error {
	path, err := config.PortFile()
	if err != nil {
		return err
	}
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(port)), 0o600)
}

// removePortFile clears the record only if it still names this
// server's port. A second `ccam serve --port N` on a different port is
// a supported thing to do, and deleting the *first* one's record on the
// way out would strand it: status stops finding it and stop can no
// longer stop it.
func removePortFile(port int) {
	path, err := config.PortFile()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(port) {
		return
	}
	_ = os.Remove(path)
}
