package accounts

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MigrationReport summarizes one run of MigrateManagedToShared, one entry
// per managed account that still needed moving.
type MigrationReport struct {
	Accounts []AccountMigration
}

// AccountMigration is what happened to one account: how many transcript
// files were copied into the shared tree, how many were already there, how
// many failed to copy, and whether the account was flipped to the
// credentials-only scheme. Err is set for a whole-account failure (e.g. its
// projects dir could not be read at all).
type AccountMigration struct {
	ID      string
	Copied  int
	Skipped int
	Failed  int
	Flipped bool
	Err     error
}

// Migrated reports whether this run actually flipped anything.
func (r MigrationReport) Migrated() bool {
	for _, a := range r.Accounts {
		if a.Flipped {
			return true
		}
	}
	return false
}

// MigrateManagedToShared performs the one-time de-isolation migration.
//
// For every managed account still on the config-dir scheme it copies that
// account's projects/ transcripts into the shared ~/.claude/projects and
// then flips it to credentials-only, so afterwards the account runs inside
// the user's own ~/.claude and its history is visible to /resume alongside
// every other account's.
//
// It is safe to run on every startup, which is how it reaches an existing
// install that only ever auto-updates:
//   - copy-only. An existing destination file is never overwritten and the
//     source is left in place, so the move is reversible until an explicit
//     prune and a crash loses nothing.
//   - the isolation flip is the per-account guard. A migrated account is
//     skipped next time. An account is flipped ONLY when every file copied
//     cleanly, so a transient read error or an unreadable file leaves it on
//     the old scheme to retry — never marked half-done.
//   - credentials and the oauthAccount identity stub in the account's own
//     directory are never read or written; no account is re-logged-in.
//
// The file copies run OUTSIDE the account lock so a large first-boot copy
// (hundreds of MB) never blocks concurrent account operations; only the
// final, quick isolation flip takes the lock, and it re-checks each account
// so a rename/remove that landed during the copy is respected.
//
// claudeDir is the user's shared config directory (~/.claude).
func (m *Manager) MigrateManagedToShared(claudeDir string) (MigrationReport, error) {
	sharedProjects := filepath.Join(claudeDir, "projects")

	list, err := m.store.Load()
	if err != nil {
		return MigrationReport{}, err
	}

	var report MigrationReport
	flip := make(map[string]bool)
	for _, a := range list {
		// Only managed accounts still on the old isolated scheme, with a
		// directory that exists, have anything to move. The default account
		// already *is* ~/.claude; a credentials-only account is already
		// migrated.
		if a.IsDefault() || a.ConfigDir == "" || a.IsolationOrDefault() == IsolationCredentialsOnly {
			continue
		}
		if info, statErr := os.Stat(a.ConfigDir); statErr != nil || !info.IsDir() {
			continue
		}

		am := AccountMigration{ID: a.ID}
		am.Copied, am.Skipped, am.Failed, am.Err =
			copyProjectsTree(filepath.Join(a.ConfigDir, "projects"), sharedProjects)
		// Flip only when the whole tree copied cleanly. A whole-account error
		// or any per-file failure leaves it config-dir to retry, rather than
		// stranding the transcripts that did not make it across.
		if am.Err == nil && am.Failed == 0 {
			flip[a.ID] = true
			am.Flipped = true
		}
		report.Accounts = append(report.Accounts, am)
	}

	if len(flip) == 0 {
		return report, nil
	}

	_, err = m.store.Mutate(func(cur []Account) ([]Account, error) {
		for i := range cur {
			// Re-check under the lock: an account could have been renamed or
			// removed while the copy ran, and one already flipped must not be
			// touched again.
			if flip[cur[i].ID] && !cur[i].IsDefault() && cur[i].IsolationOrDefault() != IsolationCredentialsOnly {
				cur[i].Isolation = IsolationCredentialsOnly
			}
		}
		return cur, nil
	})
	return report, err
}

// copyProjectsTree copies every regular file under src into dst, preserving
// the relative tree and each file's mode, never overwriting a file already
// present in dst (transcript filenames are UUIDs, so a name that already
// exists is the same session). It returns counts of files copied, skipped
// (already present), and failed (an individual file that could not be
// copied — the walk continues past it so one bad file cannot block the rest).
//
// A missing src projects/ dir is not an error: an account created but never
// used has no transcripts. Any OTHER stat error IS returned, so the caller
// leaves the account on the old scheme and retries rather than flipping it
// with nothing copied.
func copyProjectsTree(src, dst string) (copied, skipped, failed int, err error) {
	// Lstat, not Stat: a projects/ that is a SYMLINK to a tree somewhere else
	// follows as a directory here, but filepath.WalkDir below refuses to
	// descend into a symlinked root — so the walk visits the link itself, sees
	// something that is not a regular file, and reports a clean pass over zero
	// files. That looked like "migrated, nothing to do", which flipped the
	// account and later let prune delete the link out from under a tree
	// nothing had copied. Refuse to touch it instead.
	info, statErr := os.Lstat(src)
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return 0, 0, 0, fmt.Errorf("%s is a symlink; ccam will not migrate or delete a linked transcript tree", src)
	}
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return 0, 0, 0, nil // never used — nothing to move
		}
		return 0, 0, 0, statErr // real error — do not treat as "done"
	}
	if !info.IsDir() {
		return 0, 0, 0, nil
	}

	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			failed++ // could not descend into this entry; count it, keep going
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Only real transcript files. A symlink, socket, or device in the
		// tree is not a transcript and must not be followed or block the walk.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			failed++
			return nil
		}
		target := filepath.Join(dst, rel)
		if _, statErr := os.Stat(target); statErr == nil {
			skipped++ // already present — same UUID, same session
			return nil
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0o700); mkErr != nil {
			failed++
			return nil
		}
		before, beforeErr := os.Stat(path)
		if beforeErr != nil {
			failed++
			return nil
		}
		if cpErr := copyFilePreservingMode(path, target); cpErr != nil {
			failed++
			return nil
		}
		// A session that was writing while we copied left a shorter file in
		// the shared tree than the one here, and flipping the account on that
		// basis is how the rest of that conversation ends up existing only in
		// a directory prune is later told is redundant. Count it as failed so
		// the account stays on the old scheme and is retried next boot, when
		// the session will usually have ended. Prune verifies again at
		// deletion time, which is the check that actually closes this.
		if after, statErr := os.Stat(path); statErr != nil || after.Size() != before.Size() {
			failed++
			return nil
		}
		copied++
		return nil
	})
	return copied, skipped, failed, walkErr
}

// copyFilePreservingMode copies src to dst via a UNIQUE temp file in dst's
// directory, flushed to disk, then renamed into place — so a crash cannot
// leave a partial file at the final UUID name (which the skip-existing check
// would later mistake for an already-migrated transcript), and two ccam
// processes copying the same source concurrently cannot interleave into one
// temp file. The rename is atomic; the last writer wins with a complete copy.
func copyFilePreservingMode(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".ccam-migrating-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	if _, err := io.Copy(tmp, in); err != nil {
		cleanup()
		return err
	}
	// Flush the data to disk before the rename, so a power loss cannot make
	// the directory entry durable ahead of the file's bytes.
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
