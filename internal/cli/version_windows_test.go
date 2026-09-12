package cli

import (
	"strings"
	"testing"
)

func TestWindowsUpgradeInstructions(t *testing.T) {
	code, out, stderr := invoke("version", "--upgrade")
	if code != 0 || stderr != "" || !strings.Contains(out, "Get-FileHash") || !strings.Contains(out, "After this command exits") || !strings.Contains(out, "https://github.com/ydb-platform/sqlc-ydb/releases/latest") {
		t.Fatalf("%d %q %q", code, out, stderr)
	}
}
