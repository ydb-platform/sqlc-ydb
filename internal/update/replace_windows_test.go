package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsReplacementRecovery(t *testing.T) {
	for _, existingBackup := range []bool{false, true} {
		name := "rollback"
		if existingBackup {
			name = "preserve recovery backup"
		}
		t.Run(name, func(t *testing.T) {
			target := writeTarget(t)
			lock := target + ".update-lock"
			if err := os.Mkdir(lock, 0700); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(lock, "previous.exe")
			if existingBackup {
				if err := os.WriteFile(backup, []byte("recovery binary"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Missing staged data forces installation to fail after moving target.
			if err := replaceExecutable(filepath.Join(filepath.Dir(target), "missing"), target); err == nil {
				t.Fatal("expected failure")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old executable" {
				t.Fatalf("target: %q %v", data, err)
			}
			if existingBackup {
				data, err := os.ReadFile(backup)
				if err != nil || string(data) != "recovery binary" {
					t.Fatalf("backup: %q %v", data, err)
				}
				if err := os.Remove(lock); err == nil {
					t.Fatal("nonempty recovery lock was removed")
				}
			} else if _, err := os.Stat(backup); !os.IsNotExist(err) {
				t.Fatalf("backup remains after rollback: %v", err)
			}
		})
	}
}
