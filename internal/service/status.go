package service

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"ccam/internal/config"
)

// IsHTTPRunning reports whether ccam's HTTP server is currently
// answering on the port it last recorded. This is the single source of
// truth for "is the service running" across every OS: rather than
// asking each OS's service manager (which only knows whether it *tried*
// to start something), it checks that the server is actually there.
func IsHTTPRunning() (bool, error) {
	portPath, err := config.PortFile()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(portPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	port := strings.TrimSpace(string(data))
	if port == "" {
		return false, nil
	}
	if _, err := strconv.Atoi(port); err != nil {
		return false, nil
	}

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 750*time.Millisecond)
	if err != nil {
		return false, nil
	}
	conn.Close()
	return true, nil
}

// RecordSelf writes the current process's PID, so a subsequent
// Stop()/Uninstall() can find it regardless of how it was launched
// (spawned by ccam itself, a LaunchAgent, or a systemd unit).
func RecordSelf() error {
	return writePID(os.Getpid())
}
