//go:build windows

package ptyio

import (
	"context"
	"strings"
	"syscall"

	"github.com/UserExistsError/conpty"
)

type windowsSession struct {
	cpty *conpty.ConPty
}

func start(name string, args []string, env []string) (Session, error) {
	cmdLine := buildCommandLine(name, args)
	cpty, err := conpty.Start(
		cmdLine,
		conpty.ConPtyDimensions(80, 40),
		conpty.ConPtyEnv(env),
	)
	if err != nil {
		return nil, err
	}
	return &windowsSession{cpty: cpty}, nil
}

// buildCommandLine renders name+args as a single Windows command line,
// escaped per the same rules Go's own os/exec uses on Windows.
func buildCommandLine(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(name))
	for _, a := range args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}

func (s *windowsSession) Read(p []byte) (int, error)  { return s.cpty.Read(p) }
func (s *windowsSession) Write(p []byte) (int, error) { return s.cpty.Write(p) }
func (s *windowsSession) Close() error                { return s.cpty.Close() }
func (s *windowsSession) Resize(cols, rows int) error { return s.cpty.Resize(cols, rows) }

func (s *windowsSession) Wait(ctx context.Context) (int, error) {
	code, err := s.cpty.Wait(ctx)
	return int(code), err
}
