package panel

import (
	"os"
	"path/filepath"
)

// fileBackend keeps the panel in one JSON file. There is one writer — the
// `ccam panel serve` process — so the Store's mutex is the whole of the
// concurrency story and the version is unused: every save wins.
//
// The write is atomic (a temp file renamed over the target) so a crash mid-write
// leaves the previous panel intact rather than a half-written one — the failure
// mode a lending server can least afford.
type fileBackend struct {
	path string
}

func (b *fileBackend) Load() ([]byte, int64, error) {
	raw, err := os.ReadFile(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	return raw, 0, nil
}

func (b *fileBackend) Save(raw []byte, _ int64) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), ".panel-*.json")
	if err != nil {
		return false, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return false, err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(name, b.path); err != nil {
		return false, err
	}
	return true, nil
}
