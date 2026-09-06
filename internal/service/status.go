package service

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ccam/internal/config"
)

// RunningInfo describes a live ccam server.
type RunningInfo struct {
	Port    int
	Version string
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
			return nil, nil
		}
		return nil, err
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 {
		_ = os.Remove(portPath)
		return nil, nil
	}

	info := probeStatus(port)
	if info == nil {
		// Nothing recognisably ccam there; don't let the stale record
		// keep misleading the next caller.
		_ = os.Remove(portPath)
		return nil, nil
	}
	return info, nil
}

// IsHTTPRunning reports whether ccam's HTTP server is up.
func IsHTTPRunning() (bool, error) {
	info, err := Running()
	return info != nil, err
}

func probeStatus(port int) *RunningInfo {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/status")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var body struct {
		Version string `json:"version"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil
	}
	if body.Service != StatusServiceName {
		return nil // something else is on this port
	}
	return &RunningInfo{Port: port, Version: body.Version}
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
