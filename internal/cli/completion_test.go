package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			code, out, stderr := invoke("completion", shell)
			require.Zero(t, code, stderr)
			require.Empty(t, stderr)
			assert.Contains(t, out, "sqlc-ydb")
			assert.Contains(t, out, "generate")
			if shell == "fish" {
				assert.Contains(t, out, "-l against")
			} else {
				assert.Contains(t, out, "--against")
			}
			assert.NotContains(t, out, "%!")
			assert.True(t, strings.HasSuffix(out, "\n"))
		})
	}
	for _, args := range [][]string{{"completion"}, {"completion", "unknown"}, {"completion", "bash", "extra"}, {"completion", "bash", "-f", "sqlc.yaml"}, {"completion", "bash", "--no-remote"}} {
		code, _, stderr := invoke(args...)
		require.Equal(t, 1, code)
		assert.NotEmpty(t, stderr)
	}
	for _, args := range [][]string{{"completion", "--help"}, {"help", "completion"}} {
		code, out, stderr := invoke(args...)
		require.Zero(t, code, stderr)
		assert.Contains(t, out, "completion bash|zsh|fish|powershell")
	}
	assert.Equal(t, 1, Run([]string{"completion", "bash"}, brokenWriter{}, &strings.Builder{}))
}

func TestBashCompletion(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is unavailable")
	}
	code, script, stderr := invoke("completion", "bash")
	require.Zero(t, code, stderr)
	dir := t.TempDir()
	path := filepath.Join(dir, "completion.bash")
	require.NoError(t, os.WriteFile(path, []byte(script), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-config.yaml"), nil, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test with space.yaml"), nil, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test=equals.yaml"), nil, 0600))
	cmd := exec.Command("bash", "-n", path)
	require.NoError(t, cmd.Run())
	for _, tc := range []struct {
		name, words string
		index       int
		want        []string
	}{
		{"commands", "sqlc-ydb co", 1, []string{"compile", "completion"}},
		{"options", "sqlc-ydb generate --no", 2, []string{"--no-database"}},
		{"shells", "sqlc-ydb completion p", 2, []string{"powershell"}},
		{"completion options", "sqlc-ydb completion --", 2, []string{"--help"}},
		{"version options", "sqlc-ydb version --", 2, []string{"--help", "--verbose", "--no-remote", "--upgrade"}},
		{"language", "sqlc-ydb init --language py", 3, []string{"python"}},
		{"language equals", "sqlc-ydb init --language=py", 2, []string{"--language=python"}},
		{"file", "sqlc-ydb generate --file test-", 3, []string{"test-config.yaml"}},
		{"file with spaces", "sqlc-ydb generate --file test", 3, []string{"test with space.yaml", "test-config.yaml", "test=equals.yaml"}},
		{"file equals", "sqlc-ydb generate --file=test-", 2, []string{"--file=test-config.yaml"}},
		{"file name contains equals", "sqlc-ydb generate --file=test=", 2, []string{"--file=test=equals.yaml"}},
		{"against name contains equals", "sqlc-ydb verify --against=test=", 2, []string{"--against=test=equals.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := strings.Fields(tc.words)
			var bashWords []string
			for _, arg := range args {
				bashWords = append(bashWords, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
			}
			source := "source \"$1\"; compopt() { :; }; COMP_WORDS=(" + strings.Join(bashWords, " ") + "); COMP_CWORD=" + strconv.Itoa(tc.index) + "; _sqlc_ydb_complete; printf '%s\\n' \"${COMPREPLY[@]}\""
			cmd := exec.Command("bash", "-c", source, "_", path)
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
			assert.ElementsMatch(t, tc.want, strings.Split(strings.TrimSpace(string(output)), "\n"))
		})
	}
}

func TestZshCompletionSyntax(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is unavailable")
	}
	code, script, stderr := invoke("completion", "zsh")
	require.Zero(t, code, stderr)
	path := filepath.Join(t.TempDir(), "completion.zsh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0600))
	output, err := exec.Command("zsh", "-n", path).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, tc := range []struct {
		words, want string
	}{
		{"sqlc-ydb co", "completion"},
		{"sqlc-ydb verify --no", "--no-database"},
		{"sqlc-ydb completion p", "powershell"},
		{"sqlc-ydb completion --", "--help"},
		{"sqlc-ydb init --language py", "python"},
		{"sqlc-ydb generate --file config", "files"},
		{"sqlc-ydb generate --file=config", "files"},
	} {
		cmd := exec.Command("zsh", "-fc", "compdef() { :; }; compset() { :; }; _values() { print -rl -- \"$@\"; }; _files() { print files; }; source \"$1\"; words=("+tc.words+"); CURRENT=${#words}; _sqlc_ydb", "_", path)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", tc.words, output)
		assert.Contains(t, string(output), tc.want, tc.words)
		if tc.words == "sqlc-ydb completion --" {
			assert.NotContains(t, string(output), "--file")
		}
	}
}
