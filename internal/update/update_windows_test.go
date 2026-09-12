package update

import (
	"context"
	"strings"
	"testing"
)

func TestWindowsUpgradeDoesNotAccessInstallation(t *testing.T) {
	client := NewClient()
	client.ReleasesURL = "http://unused.invalid"
	client.Executable = func() (string, error) { t.Fatal("unexpected executable access"); return "", nil }
	_, err := client.Update(context.Background(), "0.1.0")
	if err == nil || !strings.Contains(err.Error(), "not supported on Windows") {
		t.Fatalf("unexpected error: %v", err)
	}
}
