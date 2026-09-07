//go:build windows

package updater

import (
	"fmt"
	"os"
)

// OldBinarySuffix names the displaced executable. Windows will not let
// a running image be overwritten, but it will let it be *renamed*, so
// the old binary is moved aside and deleted later — by the next start,
// once nothing has it open.
const OldBinarySuffix = ".old"

// replaceBinary swaps the verified download in on Windows: rename the
// running executable out of the way, move the new one into its place,
// and put the old one back if that second step fails, so a failed
// update can never leave the service with no binary at all.
func replaceBinary(from, to string) error {
	old := to + OldBinarySuffix
	// A previous update's leftover would block the rename.
	_ = os.Remove(old)

	if err := os.Rename(to, old); err != nil {
		return fmt.Errorf("moving the running %s aside: %w", to, err)
	}
	if err := os.Rename(from, to); err != nil {
		if restoreErr := os.Rename(old, to); restoreErr != nil {
			return fmt.Errorf("installing the update over %s: %w (and the previous binary could not be put back: %v)", to, err, restoreErr)
		}
		return fmt.Errorf("installing the update over %s: %w", to, err)
	}
	// Fails while this process is still running its own image; the
	// CleanupOldBinary call at the next start finishes the job.
	_ = os.Remove(old)
	return nil
}

// CleanupOldBinary removes the executable a previous update displaced.
// Called at startup, when the file is no longer anyone's running image.
func CleanupOldBinary(binaryPath string) {
	_ = os.Remove(binaryPath + OldBinarySuffix)
}
