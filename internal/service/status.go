package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ccam/internal/config"
)

// RunningInfo describes a live ccam server.
type RunningInfo struct {
	Port    int
	Version string
	PID     int
}

// Running reports whether ccam's HTTP server is answering on the port
// it last recorded, and what it said about itself.
//
// This asks /api/status and requires a recognisably-ccam response
// rather than just opening a TCP connection: a stale port file (left by
// a kill -9 or a hard power-off) plus any unrelated process that later
// happens to take that port would otherwise make every part of ccam —
// `ccam status`, the installer's health check, Start()'s "already
// running" short-circuit — confidently report a service that isn't
// there, and quietly never start the real one.
//
// A stale port file is deleted when the probe comes back negative, so
// the mistake doesn't persist.
func Running() (*RunningInfo, error) {
	portPath, err := config.PortFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(portPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No record — but a server may still be running with its
			// port file lost (a cleaned ~/.ccam, or an older version
			// that removed it). Without this ccam can't find, stop or
			// upgrade its own running server, and every start just
			// fails to bind a port it already holds.
			//
			// Only ever adopt the server this installation's own pidfile
			// names. Matching on "some ccam answers on the default
			// port" instead would make an instance with its own HOME
			// and its own port attach to an unrelated ccam — reporting
			// someone else's port as its own.
			pid, err := readPID()
			if err != nil || pid <= 0 || !processAlive(pid) {
				return nil, nil
			}
			if info, state := probeStatus(config.DefaultPort); state == listeningCcam && info.PID == pid {
				_ = writePortFile(info.Port)
				return info, nil
			}
			return nil, nil
		}
		return nil, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 {
		_ = os.Remove(portPath)
		return nil, nil
	}

	info, listening := probeStatus(port)
	if info == nil {
		if listening == notListening {
			// Nothing is on that port at all — the record is stale, so
			// stop it misleading the next caller.
			_ = os.Remove(portPath)
		}
		// Something IS listening but didn't identify as ccam. Keep the
		// record: deleting it would make Start() think the port is free
		// and try to bind it, which just fails less informatively.
		return nil, nil
	}
	return info, nil
}

// IsHTTPRunning reports whether ccam's HTTP server is up.
func IsHTTPRunning() (bool, error) {
	info, err := Running()
	return info != nil, err
}

// probeStatus asks /api/status who's there. The second return says
// whether anything was listening at all, which the caller needs to tell
// "stale record" apart from "someone else has this port".
func probeStatus(port int) (info *RunningInfo, state listenState) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/status")
	if err != nil {
		// A refused connection means nothing is there. A timeout means
		// something is, and it was merely slow — a loaded machine, or a
		// server still starting. Treating those alike would delete a
		// live server's port file and strand it: nothing rewrites that
		// file until the next restart, so status/stop/upgrade would all
		// stop finding it.
		if errors.Is(err, syscall.ECONNREFUSED) {
			return nil, notListening
		}
		return nil, listeningUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, listeningOther
	}

	var body struct {
		Version string `json:"version"`
		Service string `json:"service"`
		PID     int    `json:"pid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, listeningOther
	}

	// `service` identifies ccam, but versions before it existed replied
	// with only `version` — and the moment that distinction matters most
	// is an upgrade, when the *new* binary has to recognise the *old*
	// server it needs to stop. Refusing to would leave the old one
	// running while the new one fails to bind the port.
	if body.Service != StatusServiceName {
		// Versions before the `service` marker replied with only
		// `version`, and the moment that matters most is an upgrade,
		// when the new binary has to recognise the old server it needs
		// to stop. Accept that shape only when ccam's own pidfile names
		// a live process, so an unrelated app that happens to serve
		// /api/status can't be mistaken for ccam.
		if body.Version == "" || !pidFileNamesLiveProcess() {
			return nil, listeningOther
		}
	}
	return &RunningInfo{Port: port, Version: body.Version, PID: body.PID}, listeningCcam
}

// listenState describes what, if anything, answered on the port.
type listenState int

const (
	notListening listenState = iota
	listeningUnknown
	listeningOther
	listeningCcam
)

func pidFileNamesLiveProcess() bool {
	pid, err := readPID()
	return err == nil && pid > 0 && processAlive(pid)
}

// writePortFile records the port a discovered server is on, so the next
// caller doesn't have to rediscover it.
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

// StatusServiceName is the identifying marker /api/status returns, so
// callers can tell ccam apart from whatever else may hold the port.
const StatusServiceName = "ccam"

// RecordSelf writes the current process's PID, so a subsequent
// Stop()/Uninstall() can find it regardless of how it was launched
// (spawned by ccam itself, a LaunchAgent, or a systemd unit).
func RecordSelf() error {
	return writePID(os.Getpid())
}
