package update

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsUpgradeDoesNotAccessInstallation(t *testing.T) {
	client := NewClient()
	client.ReleasesURL = "http://unused.invalid"
	client.Executable = func() (string, error) { require.FailNow(t, "unexpected executable access"); return "", nil }
	_, err := client.Update(context.Background(), "0.1.0")
	require.False(t, err == nil || !strings.Contains(err.Error(), "not supported on Windows"), "unexpected error: %v", err)
}
