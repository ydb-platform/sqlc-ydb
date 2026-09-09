// Package cli implements the standalone sqlc-ydb command.
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ydb-platform/sqlc-engine-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/cpp"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/csharp"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/golang"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/java"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/javascript"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/php"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/python"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/codegen/rust"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/config"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/source"
)

// Release builds set Version and Commit through linker flags.
var Version = "0.0.1"
var Commit = "unknown"

const help = `sqlc-ydb generates typed code from YQL.

Usage:
  sqlc-ydb <command> [-f sqlc.yaml]

Commands:
  generate   Analyze queries and generate source code
  compile    Analyze schema and queries without generating files
  diff       Compare generated code with existing files (exit 1 on differences)
  init       Create a sqlc.yaml configuration (--v1 or --v2)
  version    Print the version (--verbose includes the build commit)

Options:
  -f, --file <path>  Use an alternate configuration file
  --no-remote       Run locally (all operations are already local)
  -h, --help        Print help
`

type arguments struct {
	command, file string
	v1, help      bool
	verbose       bool
}

func parseArgs(args []string) (arguments, error) {
	var a arguments
	v2 := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			a.help = true
		case arg == "-f" || arg == "--file":
			i++
			if i == len(args) || args[i] == "" {
				return a, errors.New("--file requires a non-empty path")
			}
			a.file = args[i]
		case strings.HasPrefix(arg, "--file="):
			a.file = strings.TrimPrefix(arg, "--file=")
			if a.file == "" {
				return a, errors.New("--file requires a non-empty path")
			}
		case arg == "--no-remote":
		case arg == "--remote":
			return a, errors.New("remote execution is not implemented; sqlc-ydb runs locally")
		case arg == "--v1":
			a.v1 = true
		case arg == "--v2":
			v2 = true
		case arg == "--verbose":
			a.verbose = true
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown option %q", arg)
		default:
			if a.command != "" {
				return a, fmt.Errorf("unexpected argument %q", arg)
			}
			a.command = arg
		}
	}
	if a.v1 && v2 {
		return a, errors.New("--v1 and --v2 are mutually exclusive")
	}
	if (a.v1 || v2) && a.command != "init" {
		return a, errors.New("--v1 and --v2 are only valid for init")
	}
	if a.verbose && a.command != "version" {
		return a, errors.New("--verbose is only valid for version")
	}
	return a, nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	a, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if a.help || a.command == "" || a.command == "help" {
		fmt.Fprint(stdout, help)
		return 0
	}
	if a.command == "version" {
		fmt.Fprintln(stdout, Version)
		if a.verbose {
			fmt.Fprintf(stdout, "commit: %s\n", Commit)
		}
		return 0
	}
	if a.command == "init" {
		if err := initialize(a, stdout); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	switch a.command {
	case "generate", "compile", "diff":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", a.command)
		return 1
	}
	c, err := config.Load(a.file)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	files, err := prepare(c, a.command != "compile")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if a.command == "compile" {
		return 0
	}
	if err := checkStaleOutputs(files); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if a.command == "diff" {
		changed, err := compare(files, stdout)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if changed {
			return 1
		}
		return 0
	}
	for _, f := range files {
		if err := writeFile(f); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}

type output struct {
	path    string
	content []byte
}

// Complete all analyses and generation before modifying any output files.
func prepare(c *config.Config, generate bool) ([]output, error) {
	var outputs []output
	seen := map[string]bool{}
	configPath, err := canonicalPath(c.Path)
	if err != nil {
		return nil, err
	}
	inputs := map[string]bool{configPath: true}
	for _, s := range c.SQL {
		schemas, err := source.Read(c.Dir, s.Schema, true)
		if err != nil {
			return nil, err
		}
		queries, err := source.Read(c.Dir, s.Queries, false)
		if err != nil {
			return nil, err
		}
		for _, list := range [][]model.Source{schemas, queries} {
			for _, src := range list {
				path, err := canonicalPath(src.Name)
				if err != nil {
					return nil, err
				}
				inputs[path] = true
			}
		}
		result, err := analyzer.Analyze(schemas, queries)
		if err != nil {
			return nil, err
		}
		if !generate {
			continue
		}
		if s.Gen.Go == nil && s.Gen.Python == nil && s.Gen.CPP == nil && s.Gen.CSharp == nil &&
			s.Gen.Java == nil && s.Gen.JavaScript == nil && s.Gen.Rust == nil && s.Gen.PHP == nil {
			return nil, errors.New("generation requires a built-in generator in gen")
		}
		add := func(dir string, files []model.File) error {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(c.Dir, dir)
			}
			for _, f := range files {
				if !filepath.IsLocal(f.Name) || f.Name == "." {
					return fmt.Errorf("generator returned invalid file name %q", f.Name)
				}
				path := filepath.Join(dir, f.Name)
				key, err := canonicalPath(path)
				if err != nil {
					return err
				}
				if seen[key] {
					return fmt.Errorf("multiple generators write %s; use distinct output directories", path)
				}
				seen[key] = true
				outputs = append(outputs, output{path: path, content: f.Content})
			}
			return nil
		}
		if g := s.Gen.Go; g != nil {
			files, err := golang.Generate(result, golang.Options{Package: g.Package, Runtime: g.SQLPackage, EmitJSONTags: g.EmitJSONTags, EmitInterface: g.EmitInterface, EmitEmptySlices: g.EmitEmptySlices})
			if err != nil {
				return nil, fmt.Errorf("Go generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if p := s.Gen.Python; p != nil {
			files, err := python.Generate(result, python.Options{Runtime: p.Runtime, EmitSyncQuerier: *p.EmitSyncQuerier, EmitAsyncQuerier: p.EmitAsyncQuerier})
			if err != nil {
				return nil, fmt.Errorf("Python generation: %w", err)
			}
			if err := add(p.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.CSharp; g != nil {
			files, err := csharp.Generate(result, csharp.Options{Namespace: g.Namespace, Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("C# generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.Java; g != nil {
			files, err := java.Generate(result, java.Options{Package: g.Package, Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("Java generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.CPP; g != nil {
			files, err := cpp.Generate(result, cpp.Options{Namespace: g.Namespace, Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("C++ generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.JavaScript; g != nil {
			files, err := javascript.Generate(result, javascript.Options{Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("JavaScript generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.Rust; g != nil {
			files, err := rust.Generate(result, rust.Options{Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("Rust generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
		if g := s.Gen.PHP; g != nil {
			files, err := php.Generate(result, php.Options{Namespace: g.Namespace, Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("PHP generation: %w", err)
			}
			if err := add(g.Out, files); err != nil {
				return nil, err
			}
		}
	}
	for _, f := range outputs {
		key, err := canonicalPath(f.path)
		if err != nil {
			return nil, err
		}
		if inputs[key] {
			return nil, fmt.Errorf("generated file would overwrite input %s", f.path)
		}
		for dir := filepath.Dir(key); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			if seen[dir] {
				return nil, fmt.Errorf("output path conflict: %s is both a generated file and a directory for %s; use distinct output directories", dir, f.path)
			}
		}
	}
	sort.Slice(outputs, func(i, j int) bool { return outputs[i].path < outputs[j].path })
	return outputs, nil
}

// Resolve existing symlinked parents even when a generated file/directory does
// not exist yet. Collision checks must use the same location as filesystem IO.
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return "", err
	}
	resolved, err = canonicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}

func compare(files []output, w io.Writer) (bool, error) {
	changed := false
	for _, f := range files {
		old, err := os.ReadFile(f.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if err == nil && bytes.Equal(old, f.content) {
			continue
		}
		changed = true
		oldLines, newLines := lines(old), lines(f.content)
		fmt.Fprintf(w, "--- %s\n+++ %s (generated)\n@@ -%d,%d +%d,%d @@\n", f.path, f.path, start(oldLines), len(oldLines), start(newLines), len(newLines))
		for _, line := range oldLines {
			fmt.Fprintf(w, "-%s\n", line)
		}
		for _, line := range newLines {
			fmt.Fprintf(w, "+%s\n", line)
		}
	}
	return changed, nil
}

func lines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
func start(s []string) int {
	if len(s) == 0 {
		return 0
	}
	return 1
}

func writeFile(f output) error {
	old, err := os.ReadFile(f.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && bytes.Equal(old, f.content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(f.path), ".sqlc-ydb-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(f.content); err != nil {
		file.Close()
		return err
	}
	if err = file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), f.path)
}

func initialize(a arguments, w io.Writer) error {
	path := a.file
	if path == "" {
		path = "sqlc.yaml"
	}
	text := `version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: query.sql
    gen:
      go:
        package: db
        out: db
        sql_package: ydb
      python:
        out: queries
        runtime: ydb
`
	if a.v1 {
		text = `version: "1"
packages:
  - name: db
    engine: ydb
    path: db
    schema: schema.sql
    queries: query.sql
    sql_package: ydb
`
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		fmt.Fprintf(w, "%s is already created\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, text)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Fprintf(w, "%s is added. Add schema.sql and query.sql, then run sqlc-ydb generate.\n", path)
	return nil
}
