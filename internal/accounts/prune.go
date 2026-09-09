package accounts

import (
	"io/fs"
	"os"
	"path/filepath"
)

// pruneKeep is everything a credentials-only account's directory still needs
// once its transcripts live in the shared ~/.claude: the credential store
// (when it is file-based rather than in the OS keychain) and the identity stub
// the usage monitor and the never-updating Windows agent read. Everything else
// under the directory is now served from ~/.claude and is safe to remove.
var pruneKeep = map[string]bool{
	".claude.json":      true, // identity stub (oauthAccount)
	".credentials.json": true, // file-based credential (Linux/Windows/SSH/no-keychain)
	".credentials":      true, // older credential filename
}

// PruneReport summarizes a prune run.
type PruneReport struct {
	Accounts []AccountPrune
	DryRun   bool
}

// AccountPrune is what was (or would be) removed for one account.
type AccountPrune struct {
	ID      string
	Removed []string
	Bytes   int64
	Err     error
}

// TotalBytes is the reclaimable total across all pruned accounts.
func (r PruneReport) TotalBytes() int64 {
	var t int64
	for _, a := range r.Accounts {
		t += a.Bytes
	}
	return t
}

// Prune reclaims the now-redundant per-account directories of credentials-only
// accounts, keeping only the credential and identity files. It is deliberately
// separate from the migration — the migration copies transcripts into the
// shared ~/.claude and leaves the originals in place, and this is the explicit
// step that deletes those originals once the user is satisfied.
//
// Only credentials-only accounts are touched (their config now comes from the
// shared ~/.claude, so nothing under their own directory is read any more), and
// the migration only marks an account credentials-only after every transcript
// copied cleanly — so the shared copy always exists before this can delete a
// source. With dryRun set it reports what it would remove without deleting. If
// ids is non-empty only those accounts are considered.
func (m *Manager) Prune(ids []string, dryRun bool) (PruneReport, error) {
	list, err := m.store.Load()
	if err != nil {
		return PruneReport{}, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}

	report := PruneReport{DryRun: dryRun}
	for _, a := range list {
		if a.IsDefault() || a.ConfigDir == "" || a.IsolationOrDefault() != IsolationCredentialsOnly {
			continue
		}
		if len(want) > 0 && !want[a.ID] {
			continue
		}
		ap := AccountPrune{ID: a.ID}
		entries, err := os.ReadDir(a.ConfigDir)
		if err != nil {
			ap.Err = err
			report.Accounts = append(report.Accounts, ap)
			continue
		}
		for _, e := range entries {
			if pruneKeep[e.Name()] {
				continue
			}
			full := filepath.Join(a.ConfigDir, e.Name())
			ap.Bytes += treeSize(full)
			ap.Removed = append(ap.Removed, e.Name())
			if !dryRun {
				if err := os.RemoveAll(full); err != nil {
					ap.Err = err
					break
				}
			}
		}
		report.Accounts = append(report.Accounts, ap)
	}
	return report, nil
}

// treeSize is the total size of the files under path (a file or a directory).
func treeSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
