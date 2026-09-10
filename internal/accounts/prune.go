package accounts

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// pruneRemovable is what prune is allowed to delete, and it is an allowlist on
// purpose. It used to be the inverse — delete everything except the credential
// and identity files — which quietly took things the migration never copied
// anywhere: history.jsonl (the account's typed prompt history), backups/,
// plugins/, a CLAUDE.md someone had put there. Those are not served from the
// shared ~/.claude; they were simply gone.
//
// So: projects/, which is verified present in the shared tree immediately
// before it is removed, plus caches Claude Code rebuilds by itself. Anything
// else stays, whatever it is. The disk is in projects/ anyway.
var pruneRemovable = map[string]bool{
	"projects":                  true, // verified into ~/.claude/projects first
	"shell-snapshots":           true, // rebuilt per session
	"telemetry":                 true,
	"statsig":                   true,
	"mcp-needs-auth-cache.json": true,
	".last-cleanup":             true,
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
	// Shared counts transcripts that existed only in this account's own
	// directory (or were longer here than in the shared tree) and had to be
	// copied across before anything could be deleted. Zero is the normal case.
	Shared int
	Err    error
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
// shared ~/.claude, so nothing under their own directory is read any more).
//
// The migration having flipped an account is NOT taken as proof that its
// transcripts are all shared, because it is not: a shell opened before the
// flip keeps the old alias and goes on writing into the account's own
// projects/ for as long as it lives, and a session that was mid-write when the
// migration copied its transcript left a shorter file in the shared tree than
// the one here. Both were then deleted as "already copied". So every file is
// verified against the shared tree at the moment of deletion, and anything
// missing or short is copied across first; if even one cannot be, the account
// is left completely alone and reported. Prune never removes what it could not
// duplicate. With dryRun set it reports what it would remove and what it would
// have to copy first, without writing anything. If ids is non-empty only those
// accounts are considered.
func (m *Manager) Prune(claudeDir string, ids []string, dryRun bool) (PruneReport, error) {
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

		// Verify (and, on a real run, top up) before deleting anything.
		shared, failed, verifyErr := shareProjects(
			filepath.Join(a.ConfigDir, "projects"),
			filepath.Join(claudeDir, "projects"),
			!dryRun,
		)
		ap.Shared = shared
		if verifyErr != nil || failed > 0 {
			ap.Err = fmt.Errorf("%d transcript(s) exist only here and could not be copied to the shared ~/.claude (%v) — nothing was removed", failed, verifyErr)
			report.Accounts = append(report.Accounts, ap)
			continue
		}

		for _, e := range entries {
			if !pruneRemovable[e.Name()] {
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

// shareProjects makes the shared tree a superset of one account's projects/
// before prune deletes it. A file missing from the shared tree is copied; a
// file that is SHORTER there than here is replaced, because that is what a
// transcript copied mid-write looks like. The comparison is directional on
// purpose: a shared file that is longer holds a conversation someone resumed
// after the migration, and this stale copy must never overwrite it.
//
// With apply false nothing is written — it just counts what would have to be.
// Returns how many files needed sharing and how many could not be.
func shareProjects(src, dst string, apply bool) (needed, failed int, err error) {
	info, statErr := os.Stat(src)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return 0, 0, nil // never used, or already pruned
		}
		return 0, 0, statErr
	}
	if !info.IsDir() {
		return 0, 0, nil
	}

	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			failed++
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			failed++
			return nil
		}
		srcInfo, infoErr := d.Info()
		if infoErr != nil {
			failed++
			return nil
		}
		target := filepath.Join(dst, rel)
		if dstInfo, dstErr := os.Stat(target); dstErr == nil && dstInfo.Size() >= srcInfo.Size() {
			return nil // already there, and at least as complete
		}
		needed++
		if !apply {
			return nil
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o700); mkErr != nil {
			failed++
			return nil
		}
		// copyFilePreservingMode writes a temp file and renames it over the
		// destination, so a short copy is replaced atomically.
		if cpErr := copyFilePreservingMode(path, target); cpErr != nil {
			failed++
			return nil
		}
		return nil
	})
	return needed, failed, walkErr
}
