// Package cli implements the standalone sqlc-ydb command.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/cpp"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/csharp"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/golang"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/java"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/kotlin"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/php"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/python"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/rust"
	"github.com/ydb-platform/sqlc-ydb/internal/codegen/typescript"
	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
	"github.com/ydb-platform/sqlc-ydb/internal/source"
	"github.com/ydb-platform/sqlc-ydb/internal/update"
)

// Version and Commit are set through linker flags in release builds.
var Version = "0.1.0"
var Commit = "unknown"

const help = `sqlc-ydb generates typed code from YQL.

Usage:
  sqlc-ydb <command> [-f sqlc.yaml]

Commands:
  generate     Analyze queries and generate source code
  compile      Analyze schema and queries without generating files
  diff         Compare generated code with existing files (exit 1 on differences)
  init         Create a sqlc.yaml configuration (version 2)
  version      Print the version and check for updates (--verbose includes the commit)

Options:
  --upgrade         Install the latest stable release in place (version only)
  -f, --file <path>  Use an alternate configuration file
  --no-remote       Skip the version update check (generation is always local)
  -h, --help        Print help
`

type arguments struct {
	command, file string
	help          bool
	verbose       bool
	noRemote      bool
	upgrade       bool
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
			a.noRemote = true
		case arg == "--remote":
			return a, errors.New("remote execution is not implemented; sqlc-ydb runs locally")
		case arg == "--v2":
			v2 = true
		case arg == "--verbose":
			a.verbose = true
		case arg == "--upgrade":
			a.upgrade = true
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown option %q", arg)
		default:
			if a.command != "" {
				return a, fmt.Errorf("unexpected argument %q", arg)
			}
			a.command = arg
		}
	}
	if v2 && a.command != "init" {
		return a, errors.New("--v2 is only valid for init")
	}
	if a.verbose && a.command != "version" {
		return a, errors.New("--verbose is only valid for version")
	}
	if a.upgrade && a.command != "version" {
		return a, errors.New("--upgrade is only valid for version")
	}
	if a.noRemote && a.upgrade {
		return a, errors.New("--upgrade requires network access; remove --no-remote")
	}
	if a.verbose && a.upgrade {
		return a, errors.New("--verbose cannot be combined with --upgrade")
	}
	return a, nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, update.NewClient())
}

func run(args []string, stdout, stderr io.Writer, updater *update.Client) int {
	fail := func(err error) int {
		// The command already failed; reporting that failure is best effort.
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	a, err := parseArgs(args)
	if err != nil {
		return fail(err)
	}
	if a.help || a.command == "" || a.command == "help" {
		if _, err := fmt.Fprint(stdout, help); err != nil {
			return fail(err)
		}
		return 0
	}
	if a.command == "version" && !a.upgrade {
		if _, err := fmt.Fprintln(stdout, Version); err != nil {
			return fail(err)
		}
		if a.verbose {
			if _, err := fmt.Fprintf(stdout, "commit: %s\n", Commit); err != nil {
				return fail(err)
			}
		}
		if !a.noRemote {
			latest, err := updater.Latest(context.Background())
			if err == nil && update.Newer(latest, Version) {
				if _, err := fmt.Fprintf(stdout, "New version available: %s. Run sqlc-ydb version --upgrade to install it.\n", latest); err != nil {
					return fail(err)
				}
			}
		}
		return 0
	}
	if a.upgrade {
		if runtime.GOOS == "windows" {
			_, err := fmt.Fprintln(stdout, "Automatic upgrades are not supported on Windows.\nDownload the Windows ZIP for your architecture and SHA256SUMS from:\nhttps://github.com/ydb-platform/sqlc-ydb/releases/latest\nVerify the ZIP with Get-FileHash -Algorithm SHA256 and extract sqlc-ydb.exe.\nAfter this command exits, close other sqlc-ydb processes and replace the installed executable (the symlink target, if applicable).")
			if err != nil {
				return fail(err)
			}
			return 0
		}
		result, err := updater.Update(context.Background(), Version)
		if err != nil {
			return fail(err)
		}
		if result.Updated {
			_, err = fmt.Fprintf(stdout, "Updated sqlc-ydb to %s.\nLocation: %s\n", result.Version, result.Path)
		} else {
			_, err = fmt.Fprintf(stdout, "sqlc-ydb %s is already up to date.\n", result.Version)
		}
		if err != nil {
			return fail(err)
		}
		return 0
	}
	if a.command == "init" {
		if err := initialize(a, stdout); err != nil {
			return fail(err)
		}
		return 0
	}
	switch a.command {
	case "generate", "compile", "diff":
	default:
		return fail(fmt.Errorf("unknown command %q", a.command))
	}
	c, err := config.Load(a.file)
	if err != nil {
		return fail(err)
	}
	files, err := prepare(c, a.command != "compile")
	if err != nil {
		return fail(err)
	}
	if a.command == "compile" {
		return 0
	}
	if err := checkStaleOutputs(files); err != nil {
		return fail(err)
	}
	if a.command == "diff" {
		changed, err := compare(files, stdout)
		if err != nil {
			return fail(err)
		}
		if changed {
			return 1
		}
		return 0
	}
	for _, f := range files {
		if err := writeFile(f); err != nil {
			return fail(err)
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
			s.Gen.Java == nil && s.Gen.Kotlin == nil && s.Gen.TypeScript == nil && s.Gen.Rust == nil && s.Gen.PHP == nil {
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
			files, err := python.Generate(result, python.Options{Runtime: p.Runtime, EmitAsyncQuerier: p.EmitAsyncQuerier})
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
		if g := s.Gen.Kotlin; g != nil {
			files, err := kotlin.Generate(result, kotlin.Options{Package: g.Package, Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("Kotlin generation: %w", err)
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
		if g := s.Gen.TypeScript; g != nil {
			files, err := typescript.Generate(result, typescript.Options{Runtime: g.Runtime})
			if err != nil {
				return nil, fmt.Errorf("TypeScript generation: %w", err)
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
		if _, err := fmt.Fprintf(w, "--- %s\n+++ %s (generated)\n@@ -%d,%d +%d,%d @@\n", f.path, f.path, start(oldLines), len(oldLines), start(newLines), len(newLines)); err != nil {
			return false, err
		}
		for _, line := range oldLines {
			if _, err := fmt.Fprintf(w, "-%s\n", line); err != nil {
				return false, err
			}
		}
		for _, line := range newLines {
			if _, err := fmt.Fprintf(w, "+%s\n", line); err != nil {
				return false, err
			}
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
	defer func() {
		// Keep the write/rename error; a successful rename already removed this path.
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if _, err = file.Write(f.content); err != nil {
		return err
	}
	if err = file.Chmod(0644); err != nil {
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
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		_, err = fmt.Fprintf(w, "%s is already created\n", path)
		return err
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
	_, err = fmt.Fprintf(w, "%s is added. Add schema.sql and query.sql, then run sqlc-ydb generate.\n", path)
	return err
}
