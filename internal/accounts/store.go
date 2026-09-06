package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is a JSON-file-backed, mutex-guarded account list. Writes are
// atomic (write to a temp file, then rename) so a crash or concurrent
// reader never observes a half-written file.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore opens (without yet reading) the store backed by path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

type fileFormat struct {
	Accounts []Account `json:"accounts"`
}

// Load returns the current account list. A missing file is treated as an
// empty list, not an error, so a fresh install needs no bootstrap step.
func (s *Store) Load() ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() ([]Account, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Account{}, nil
		}
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.path, err)
	}
	if f.Accounts == nil {
		return []Account{}, nil
	}
	return f.Accounts, nil
}

func (s *Store) save(list []Account) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileFormat{Accounts: list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Mutate loads the current list, lets fn modify it, and atomically
// persists the result — the unit every accounts.Manager operation is
// built from, so concurrent HTTP requests never race on the file.
func (s *Store) Mutate(fn func(list []Account) ([]Account, error)) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	list, err = fn(list)
	if err != nil {
		return nil, err
	}
	if err := s.save(list); err != nil {
		return nil, err
	}
	return list, nil
}
