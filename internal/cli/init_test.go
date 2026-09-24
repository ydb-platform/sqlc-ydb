package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

func TestInitHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlc.yaml")
	for _, args := range [][]string{
		{"init", "--help"},
		{"help", "init"},
		{"help", "init", "--language", "go", "--runtime", "ydb"},
		{"--help", "init"},
		{"init", "--help", "--language", "go", "--runtime", "ydb"},
		{"init", "--language=go", "--runtime=ydb", "--help"},
	} {
		code, out, stderr := invoke(append(args, "-f", path)...)
		require.Zero(t, code, "%v: %s", args, stderr)
		require.Empty(t, stderr, "%v", args)
		for _, generator := range config.Generators() {
			want := fmt.Sprintf("%-12s %s (default: %s)", generator.Language, strings.Join(generator.Runtimes, ", "), generator.DefaultRuntime)
			assert.Contains(t, out, want, "%v", args)
			for alias, canonical := range generator.RuntimeAliases {
				assert.Contains(t, out, alias+" is an alias for "+canonical, "%v", args)
			}
		}
		if strings.Contains(strings.Join(args, " "), "go") {
			for _, want := range []string{"gen.go", "runtime: ydb", "emit_json_tags", "default: false", "database/sql", "Selected for this configuration: ydb"} {
				assert.Contains(t, out, want, "%v", args)
			}
		}
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "help touched config")
	}
}

func TestInitEveryRuntime(t *testing.T) {
	for _, generator := range config.Generators() {
		for _, runtime := range generator.Runtimes {
			t.Run(generator.Language+"/"+runtime, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sqlc.yaml")
				args := []string{"init", "--language=" + generator.Language, "--runtime=" + runtime, "--file=" + path}
				code, _, stderr := invoke(args...)
				require.Zero(t, code, stderr)
				_, err := config.Load(path)
				require.NoError(t, err, "created invalid configuration")
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				for _, option := range generator.Options {
					assert.Contains(t, string(data), option.Name+":")
				}
				code, out, stderr := invoke(append(args, "--help")...)
				require.Zero(t, code, stderr)
				require.Contains(t, out, "gen."+generator.Language)
				assert.Contains(t, out, "Selected for this configuration: "+runtime)
				for _, option := range generator.Options {
					heading := fmt.Sprintf("%s (%s; default: %s)", option.Name, option.Type, option.Default)
					if option.Required {
						heading = fmt.Sprintf("%s (%s; required)", option.Name, option.Type)
					}
					for _, want := range []string{heading, option.Description} {
						assert.Contains(t, out, want)
					}
					if len(option.Values) > 0 {
						assert.Contains(t, out, "Values: "+strings.Join(option.Values, ", "), option.Name)
					}
				}
				code, _, stderr = invoke(append(args, "--all-options")...)
				require.Zero(t, code, stderr)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, data, after, "init modified an existing file")
			})
		}
	}
}

func TestExplicitDashPrefixedFile(t *testing.T) {
	for _, args := range [][]string{
		{"init", "--file=-cfg.yaml"},
		{"init", "-f", "./-cfg.yaml"},
	} {
		parsed, err := parseArgs(args)
		require.NoError(t, err, "%v", args)
		require.Equal(t, "-cfg.yaml", filepath.Base(parsed.file), "%v", args)
	}
}

func TestInitInvalidSelectionDoesNotWrite(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"init", "--file", "--help"}, "--file requires a non-empty path"},
		{[]string{"init", "-f", "--language", "go"}, "--file requires a non-empty path"},
		{[]string{"init", "--language"}, "--language requires"},
		{[]string{"init", "--language", "--help"}, "--language requires"},
		{[]string{"init", "--language="}, "--language requires"},
		{[]string{"init", "--language", ""}, "--language requires"},
		{[]string{"init", "--runtime"}, "--runtime requires"},
		{[]string{"init", "--runtime="}, "--runtime requires"},
		{[]string{"init", "--runtime", "--help"}, "--runtime requires"},
		{[]string{"init", "--runtime", "ydb"}, "--runtime requires --language"},
		{[]string{"init", "--language", "javascript"}, "javascript"},
		{[]string{"init", "--language", "go", "--runtime", "dapper", "--help"}, "dapper"},
		{[]string{"generate", "--language", "go"}, "only valid for init"},
		{[]string{"compile", "--runtime", "ydb"}, "only valid for init"},
		{[]string{"version", "--all-options"}, "only valid for init"},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "sqlc.yaml")
		// Put the file flag first so a missing final value remains missing.
		args := append([]string{"--file", path}, tc.args...)
		code, _, stderr := invoke(args...)
		assert.Equal(t, 1, code, "%v: %s", tc.args, stderr)
		assert.Contains(t, stderr, tc.want, "%v", tc.args)
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "invalid command touched config")
	}
}

func TestInitAllOptionsAndDefaults(t *testing.T) {
	for _, language := range []string{"", "go"} {
		var outputs [][]byte
		for _, all := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "sqlc.yaml")
			args := []string{"init", "-f", path}
			if language != "" {
				args = append(args, "--language", language)
			}
			if all {
				args = append(args, "--all-options")
			}
			code, _, stderr := invoke(args...)
			require.Zero(t, code, stderr)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			outputs = append(outputs, data)
		}
		require.Equal(t, outputs[0], outputs[1], "--all-options changed default output for %q", language)
		require.False(t, language == "" && (!bytes.Contains(outputs[0], []byte("python:")) || !bytes.Contains(outputs[0], []byte("sql_package: ydb"))), "legacy default must include Go ydb and Python")
	}
}

func TestInitNativeRuntimeAliases(t *testing.T) {
	for _, language := range []string{"cpp", "java", "kotlin"} {
		path := filepath.Join(t.TempDir(), "sqlc.yaml")
		code, _, stderr := invoke("init", "--language", language, "--runtime", "native", "-f", path)
		require.Zero(t, code, stderr)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(data), "runtime: ydb", "%s alias did not resolve to ydb", language)
	}
}
