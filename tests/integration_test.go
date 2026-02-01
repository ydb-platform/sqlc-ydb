package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
)

type Testcase struct {
	Name       string
	Path       string
	ConfigName string
	Stderr     []byte
	Exec       *Exec
}

type Exec struct {
	Command string            `json:"command"`
	Env     map[string]string `json:"env"`
}

func parseStderr(t *testing.T, dir string) []byte {
	t.Helper()
	path := filepath.Join(dir, "stderr.txt")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	blob, err := os.ReadFile(path)
	require.NoError(t, err)
	return blob
}

func parseExec(t *testing.T, dir string) *Exec {
	t.Helper()
	path := filepath.Join(dir, "exec.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	var e Exec
	blob, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read %s", path)
	require.NoError(t, json.Unmarshal(blob, &e), "failed to unmarshal %s", path)
	if e.Command == "" {
		e.Command = "generate"
	}
	return &e
}

func FindTests(t *testing.T, root string) []*Testcase {
	var tcs []*Testcase
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		if info.Name() == "sqlc.json" || info.Name() == "sqlc.yaml" || info.Name() == "sqlc.yml" {
			dir := filepath.Dir(path)
			tcs = append(tcs, &Testcase{
				Path:       dir,
				Name:       strings.TrimPrefix(dir, root+string(filepath.Separator)),
				ConfigName: info.Name(),
				Stderr:     parseStderr(t, dir),
				Exec:       parseExec(t, dir),
			})
			return filepath.SkipDir
		}
		return nil
	})
	require.NoError(t, err)
	return tcs
}

func lineEndings() cmp.Option {
	return cmp.Transformer("LineEndings", func(in string) string {
		return strings.Replace(in, "\r\n", "\n", -1)
	})
}

func stderrTransformer() cmp.Option {
	return cmp.Transformer("Stderr", func(in string) string {
		s := strings.Replace(in, "\r", "", -1)
		return strings.Replace(s, "\\", "/", -1)
	})
}

func cmpDirectory(t *testing.T, dir string, actual map[string]string) {
	expected := map[string]string{}
	var ff = func(path string, file os.FileInfo, err error) error {
		require.NoError(t, err)
		if file.IsDir() {
			return nil
		}
		// Support multiple file extensions
		ext := filepath.Ext(path)
		supportedExts := []string{".go", ".py", ".kt", ".java", ".ts", ".rs", ".php", ".cs"}
		hasSupportedExt := false
		for _, supportedExt := range supportedExts {
			if ext == supportedExt {
				hasSupportedExt = true
				break
			}
		}
		if !hasSupportedExt {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_test.py") {
			return nil
		}
		if filepath.Base(path) == "sqlc.json" || filepath.Base(path) == "sqlc.yaml" || filepath.Base(path) == "sqlc.yml" {
			return nil
		}
		blob, err := os.ReadFile(path)
		require.NoError(t, err)
		expected[path] = string(blob)
		return nil
	}
	require.NoError(t, filepath.Walk(dir, ff))

	opts := []cmp.Option{
		cmpopts.EquateEmpty(),
		lineEndings(),
	}

	if !cmp.Equal(expected, actual, opts...) {
		for name, contents := range expected {
			require.NotEmpty(t, actual[name], "%s is empty", name)
			if diff := cmp.Diff(contents, actual[name], opts...); diff != "" {
				t.Errorf("%s differed (-want +got):\n%s", name, diff)
			}
		}
	}
}

// TestIntegration tests the plugin with real sqlc
func TestIntegration(t *testing.T) {
	ctx := context.Background()

	// Find sqlc binary
	sqlcPath := findSQLCBinary(t)
	if sqlcPath == "" {
		t.Skip("sqlc binary not found. Build sqlc from engine-plugin first.")
	}

	// Find plugin
	pluginPath := findPluginBinary(t)
	if pluginPath == "" {
		t.Skip("plugin binary not found. Run 'make build' first.")
	}

	// Find all codegen plugins
	codegenPlugins := findCodegenPlugins(t)
	if len(codegenPlugins) == 0 {
		t.Skip("codegen plugins not found. Run 'make build-plugins' first.")
	}

	// Add plugins to PATH
	originalPath := os.Getenv("PATH")
	pluginDir := filepath.Dir(pluginPath)
	codegenPluginsDir := filepath.Dir(codegenPlugins[0])
	os.Setenv("PATH", pluginDir+string(filepath.ListSeparator)+codegenPluginsDir+string(filepath.ListSeparator)+originalPath)
	defer os.Setenv("PATH", originalPath)

	// Find all test cases
	testdataDir := filepath.Join("testdata")
	for _, tc := range FindTests(t, testdataDir) {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			var stderr bytes.Buffer
			var err error

			path, _ := filepath.Abs(tc.Path)
			args := tc.Exec
			if args == nil {
				args = &Exec{Command: "generate"}
			}
			expected := string(tc.Stderr)

			// Run sqlc generate
			cmd := exec.CommandContext(ctx, sqlcPath, "generate")
			cmd.Dir = path
			cmd.Stderr = &stderr
			cmd.Env = os.Environ()

			// Add environment variables from exec.json
			for k, v := range args.Env {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}

			err = cmd.Run()

			if err != nil {
				// If error is expected, check stderr
				if len(expected) > 0 {
					diff := cmp.Diff(
						strings.TrimSpace(expected),
						strings.TrimSpace(stderr.String()),
						stderrTransformer(),
					)
					require.Empty(t, diff, "stderr differed (-want +got):\n%s", diff)
					return
				}
				require.NoError(t, err, "sqlc generate failed: %s", stderr.String())
			}

			// If no error expected but it occurred
			require.Empty(t, expected, "expected error but got none")

			// Check git diff - should be empty
			checkGitDiff(t, path)
		})
	}
}

// checkGitDiff checks that git diff is empty after code generation
func checkGitDiff(t *testing.T, dir string) {
	t.Helper()

	// Check that we are in a git repository
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		// Not in git repository, skip check
		return
	}

	// Get git diff
	cmd = exec.Command("git", "diff", "--exit-code", ".")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()

	if err != nil {
		// There are changes - show diff
		diffCmd := exec.Command("git", "diff", ".")
		diffCmd.Dir = dir
		diffOutput, _ := diffCmd.Output()
		require.NoError(t, err, "git diff is not empty after code generation:\n%s\n\nDiff:\n%s", string(output), string(diffOutput))
	}
}

func findSQLCBinary(t *testing.T) string {
	// Try to find sqlc in different locations
	paths := []string{
		filepath.Join("..", "..", "engine-plugin", "bin", "sqlc"),
		filepath.Join("..", "engine-plugin", "bin", "sqlc"),
		"sqlc",
	}
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	return ""
}

func findPluginBinary(t *testing.T) string {
	paths := []string{
		filepath.Join("..", "bin", "sqlc-engine-ydb"),
		filepath.Join("..", "sqlc-engine-ydb"),
		"sqlc-engine-ydb",
	}
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	return ""
}

func findCodegenPlugins(t *testing.T) []string {
	pluginNames := []string{
		"sqlc-gen-ydb-go",
		"sqlc-gen-ydb-python",
		"sqlc-gen-ydb-java",
		"sqlc-gen-ydb-js",
		"sqlc-gen-ydb-rust",
		"sqlc-gen-ydb-php",
		"sqlc-gen-ydb-dotnet",
	}

	var found []string
	paths := []string{
		filepath.Join("..", "bin"),
		".",
	}

	for _, pluginName := range pluginNames {
		for _, basePath := range paths {
			p := filepath.Join(basePath, pluginName)
			if abs, err := filepath.Abs(p); err == nil {
				if _, err := os.Stat(abs); err == nil {
					found = append(found, abs)
					break
				}
			}
			if path, err := exec.LookPath(pluginName); err == nil {
				found = append(found, path)
				break
			}
		}
	}
	return found
}

func collectGeneratedFiles(t *testing.T, dir string) map[string]string {
	result := make(map[string]string)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		require.NoError(t, err)
		if info.IsDir() {
			return nil
		}
		// Support multiple file extensions
		ext := filepath.Ext(path)
		supportedExts := []string{".go", ".py", ".kt", ".java", ".ts", ".rs", ".php", ".cs"}
		hasSupportedExt := false
		for _, supportedExt := range supportedExts {
			if ext == supportedExt {
				hasSupportedExt = true
				break
			}
		}
		if !hasSupportedExt {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_test.py") {
			return nil
		}
		// Skip config files
		if filepath.Base(path) == "sqlc.json" || filepath.Base(path) == "sqlc.yaml" || filepath.Base(path) == "sqlc.yml" {
			return nil
		}
		blob, err := os.ReadFile(path)
		require.NoError(t, err)
		result[path] = string(blob)
		return nil
	})
	require.NoError(t, err)
	return result
}
