package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

func TestInitHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlc.yaml")
	for _, args := range [][]string{
		{"init", "--help"},
		{"--help", "init"},
		{"init", "--help", "--language", "go", "--runtime", "ydb"},
		{"init", "--language=go", "--runtime=ydb", "--help"},
	} {
		code, out, stderr := invoke(append(args, "-f", path)...)
		if code != 0 || stderr != "" {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		for _, generator := range config.Generators() {
			want := fmt.Sprintf("%-12s %s (default: %s)", generator.Language, strings.Join(generator.Runtimes, ", "), generator.DefaultRuntime)
			if !strings.Contains(out, want) {
				t.Errorf("%v: missing discovery entry %q", args, want)
			}
			for alias, canonical := range generator.RuntimeAliases {
				if !strings.Contains(out, alias+" is an alias for "+canonical) {
					t.Errorf("%v: missing alias %s for %s", args, alias, generator.Language)
				}
			}
		}
		if strings.Contains(strings.Join(args, " "), "go") {
			for _, want := range []string{"gen.go", "runtime: ydb", "emit_json_tags", "default: false", "database/sql", "Selected for this configuration: ydb"} {
				if !strings.Contains(out, want) {
					t.Errorf("%v: help missing %q: %s", args, want, out)
				}
			}
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("help touched config: %v", err)
		}
	}
}

func TestInitEveryRuntime(t *testing.T) {
	for _, generator := range config.Generators() {
		for _, runtime := range generator.Runtimes {
			t.Run(generator.Language+"/"+runtime, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sqlc.yaml")
				args := []string{"init", "--language=" + generator.Language, "--runtime=" + runtime, "--file=" + path}
				if code, _, stderr := invoke(args...); code != 0 {
					t.Fatal(stderr)
				}
				if _, err := config.Load(path); err != nil {
					t.Fatalf("created invalid configuration: %v", err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, option := range generator.Options {
					if !bytes.Contains(data, []byte(option.Name+":")) {
						t.Errorf("config missing option %s", option.Name)
					}
				}
				code, out, stderr := invoke(append(args, "--help")...)
				if code != 0 || !strings.Contains(out, "gen."+generator.Language) {
					t.Fatalf("runtime help: %d %s %s", code, out, stderr)
				}
				if !strings.Contains(out, "Selected for this configuration: "+runtime) {
					t.Errorf("help omitted selected runtime %s", runtime)
				}
				for _, option := range generator.Options {
					heading := fmt.Sprintf("%s (%s; default: %s)", option.Name, option.Type, option.Default)
					if option.Required {
						heading = fmt.Sprintf("%s (%s; required)", option.Name, option.Type)
					}
					for _, want := range []string{heading, option.Description} {
						if !strings.Contains(out, want) {
							t.Errorf("help omitted %q", want)
						}
					}
					if len(option.Values) > 0 && !strings.Contains(out, "Values: "+strings.Join(option.Values, ", ")) {
						t.Errorf("help omitted supported values for %s", option.Name)
					}
				}
				if code, _, stderr := invoke(append(args, "--all-options")...); code != 0 {
					t.Fatal(stderr)
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(data, after) {
					t.Fatal("init modified an existing file")
				}
			})
		}
	}
}

func TestInitializeRejectsInvalidGenerator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlc.yaml")
	if err := initialize(arguments{file: path, language: "unknown"}, io.Discard); err == nil {
		t.Fatal("accepted unsupported generator")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid generator touched config: %v", err)
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
		if code, _, stderr := invoke(args...); code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: %d %q; want %q", tc.args, code, stderr, tc.want)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("invalid command touched config: %v", err)
		}
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
			if code, _, stderr := invoke(args...); code != 0 {
				t.Fatal(stderr)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			outputs = append(outputs, data)
		}
		if !bytes.Equal(outputs[0], outputs[1]) {
			t.Fatalf("--all-options changed default output for %q", language)
		}
		if language == "" && (!bytes.Contains(outputs[0], []byte("python:")) || !bytes.Contains(outputs[0], []byte("sql_package: ydb"))) {
			t.Fatal("legacy default must include Go ydb and Python")
		}
	}
}

func TestInitNativeRuntimeAliases(t *testing.T) {
	for _, language := range []string{"cpp", "java", "kotlin"} {
		path := filepath.Join(t.TempDir(), "sqlc.yaml")
		if code, _, stderr := invoke("init", "--language", language, "--runtime", "native", "-f", path); code != 0 {
			t.Fatal(stderr)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte("runtime: ydb")) {
			t.Fatalf("%s alias did not resolve to ydb: %s, %v", language, data, err)
		}
	}
}
