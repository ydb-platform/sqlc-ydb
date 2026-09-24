package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsUpgradeInstructions(t *testing.T) {
	code, out, stderr := invoke("version", "--upgrade")
	require.Zero(t, code, stderr)
	require.Empty(t, stderr)
	require.Contains(t, out, "Get-FileHash")
	require.Contains(t, out, "After this command exits")
	require.Contains(t, out, "https://github.com/ydb-platform/sqlc-ydb/releases/latest")
}
