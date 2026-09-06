//go:build !windows

package ptyio

import (
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixSession struct {
	f   *os.File
	cmd *exec.Cmd
	// done is closed once cmd.Wait has returned, carrying the result.
	done     chan struct{}
	exitCode int
	waitErr  error
}

func start(name string, args []string, env []string, dir string) (Session, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Dir = dir

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: DefaultCols, Rows: DefaultRows})
	if err != nil {
		return nil, err
	}

	s := &unixSession{f: f, cmd: cmd, done: make(chan struct{})}
	go func() {
		s.waitErr = cmd.Wait()
		if s.waitErr != nil {
			if exitErr, ok := s.waitErr.(*exec.ExitError); ok {
				s.exitCode = exitErr.ExitCode()
			} else {
				s.exitCode = -1
			}
		}
		close(s.done)
	}()
	return s, nil
}

func (s *unixSession) Read(p []byte) (int, error)  { return s.f.Read(p) }
func (s *unixSession) Write(p []byte) (int, error) { return s.f.Write(p) }

// Close releases the pty and ensures the child process doesn't linger:
// closing the master alone doesn't guarantee a blocked/non-I/O child
// (e.g. one stuck waiting on something other than its terminal) exits.
func (s *unixSession) Close() error {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.f.Close()
}

func (s *unixSession) Resize(cols, rows int) error {
	return pty.Setsize(s.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (s *unixSession) Wait(ctx context.Context) (int, error) {
	select {
	case <-s.done:
		if s.waitErr != nil {
			if _, ok := s.waitErr.(*exec.ExitError); ok {
				return s.exitCode, nil // non-zero exit is not itself a Wait error
			}
			return s.exitCode, s.waitErr
		}
		return 0, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}
