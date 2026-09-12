package update

import (
	"errors"
	"fmt"
	"os"
)

func replaceExecutable(staged, target string) error {
	// Windows can rename a running executable but cannot replace it directly.
	// Use the unique staging name so a previous backup is never overwritten.
	// The old image may remain locked until this process exits.
	backup := staged + ".old"
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("update backup path is not available: %s", backup)
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			return fmt.Errorf("install failed: %w; restore failed: %w; previous binary: %s", err, restoreErr, backup)
		}
		return err
	}
	_ = os.Remove(backup)
	return nil
}
