// Package config loads the supported sqlc configuration contract.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ydb-platform/sqlc-ydb/internal/yql/builtins"

	"go.yaml.in/yaml/v3"
)

type Paths []string

func (p *Paths) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag != "!!str" || n.Value == "" {
			return fmt.Errorf("line %d: expected a non-empty path", n.Line)
		}
		*p = []string{n.Value}
	case yaml.SequenceNode:
		for _, child := range n.Content {
			if child.Kind != yaml.ScalarNode || child.Tag != "!!str" || child.Value == "" {
				return fmt.Errorf("line %d: expected a non-empty path", child.Line)
			}
			*p = append(*p, child.Value)
		}
	default:
		return fmt.Errorf("line %d: expected a path or list of paths", n.Line)
	}
	return nil
}

type Go struct {
	Package         string `yaml:"package"`
	Out             string `yaml:"out"`
	SQLPackage      string `yaml:"sql_package"`
	EmitJSONTags    bool   `yaml:"emit_json_tags"`
	EmitInterface   bool   `yaml:"emit_interface"`
	EmitEmptySlices bool   `yaml:"emit_empty_slices"`
}

type Python struct {
	Package          yaml.Node `yaml:"package"` // Retained only to diagnose the formerly ignored option.
	Out              string    `yaml:"out"`
	Runtime          string    `yaml:"runtime"`
	EmitSyncQuerier  *bool     `yaml:"emit_sync_querier"`
	EmitAsyncQuerier bool      `yaml:"emit_async_querier"`
}

type CPP struct {
	Namespace string `yaml:"namespace"`
	Out       string `yaml:"out"`
	Runtime   string `yaml:"runtime"`
}

type CSharp struct {
	Namespace string `yaml:"namespace"`
	Out       string `yaml:"out"`
	Runtime   string `yaml:"runtime"`
}

type Java struct {
	Package string `yaml:"package"`
	Out     string `yaml:"out"`
	Runtime string `yaml:"runtime"`
}

type Kotlin struct {
	Package string `yaml:"package"`
	Out     string `yaml:"out"`
	Runtime string `yaml:"runtime"`
}

type TypeScript struct {
	Out     string `yaml:"out"`
	Runtime string `yaml:"runtime"`
}

type Rust struct {
	Out     string `yaml:"out"`
	Runtime string `yaml:"runtime"`
}

type PHP struct {
	Namespace string `yaml:"namespace"`
	Out       string `yaml:"out"`
	Runtime   string `yaml:"runtime"`
}

type Gen struct {
	Go         *Go         `yaml:"go"`
	Python     *Python     `yaml:"python"`
	CPP        *CPP        `yaml:"cpp"`
	CSharp     *CSharp     `yaml:"csharp"`
	Java       *Java       `yaml:"java"`
	Kotlin     *Kotlin     `yaml:"kotlin"`
	TypeScript *TypeScript `yaml:"typescript"`
	Rust       *Rust       `yaml:"rust"`
	PHP        *PHP        `yaml:"php"`
}

type FunctionArgument struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Optional bool   `yaml:"optional"`
	AutoMap  bool   `yaml:"auto_map"`
}

type Function struct {
	Name    string             `yaml:"name"`
	Args    []FunctionArgument `yaml:"args"`
	Returns string             `yaml:"returns"`
}

type Analyzer struct {
	Functions []Function `yaml:"functions"`
}

type SQL struct {
	Name     string    `yaml:"name"`
	Engine   string    `yaml:"engine"`
	Schema   Paths     `yaml:"schema"`
	Queries  Paths     `yaml:"queries"`
	Analyzer Analyzer  `yaml:"analyzer"`
	Gen      Gen       `yaml:"gen"`
	Codegen  yaml.Node `yaml:"codegen"`
}

type Config struct {
	Version string    `yaml:"version"`
	SQL     []SQL     `yaml:"sql"`
	Plugins yaml.Node `yaml:"plugins"`
	Engines yaml.Node `yaml:"engines"`
	Path    string    `yaml:"-"`
	Dir     string    `yaml:"-"`
}

// Load resolves source/output paths relative to the configuration, not the cwd.
func Load(path string) (*Config, error) {
	if path == "" {
		for _, candidate := range []string{"sqlc.yaml", "sqlc.yml", "sqlc.json"} {
			_, err := os.Stat(candidate)
			if err == nil {
				path = candidate
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		if path == "" {
			return nil, errors.New("no sqlc.yaml, sqlc.yml or sqlc.json configuration found")
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path, c.Dir = abs, filepath.Dir(abs)
	return c, nil
}

func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("configuration must contain exactly one document")
	}
	if c.Plugins.Kind != 0 || c.Engines.Kind != 0 {
		return nil, pluginError()
	}
	if c.Version != "2" {
		return nil, errors.New("version must be \"2\"")
	}
	if len(c.SQL) == 0 {
		return nil, errors.New("configuration contains no SQL query sets")
	}
	for i := range c.SQL {
		s := &c.SQL[i]
		if s.Codegen.Kind != 0 {
			return nil, pluginError()
		}
		if s.Engine != "ydb" {
			return nil, fmt.Errorf("sql[%d]: engine must be ydb; other engines are not supported", i)
		}
		if len(s.Schema) == 0 || len(s.Queries) == 0 {
			return nil, fmt.Errorf("sql[%d]: schema and queries paths are required", i)
		}
		if err := validateFunctions(i, s.Analyzer.Functions); err != nil {
			return nil, err
		}
		if g := s.Gen.Go; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.go.out is required", i)
			}
			if g.Package == "" {
				g.Package = filepath.Base(filepath.Clean(g.Out))
			}
			resolved, err := resolveRuntime("go", g.SQLPackage)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.SQLPackage = resolved
		}
		if p := s.Gen.Python; p != nil {
			if p.Package.Kind != 0 {
				return nil, fmt.Errorf("sql[%d].gen.python.package is unsupported; remove it: the Python package directory is selected with out", i)
			}
			if p.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.python.out is required", i)
			}
			resolved, err := resolveRuntime("python", p.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			p.Runtime = resolved
			if p.EmitSyncQuerier == nil {
				v := defaultSyncQuerier
				p.EmitSyncQuerier = &v
			}
			if !*p.EmitSyncQuerier && !p.EmitAsyncQuerier {
				return nil, errors.New("Python requires emit_sync_querier or emit_async_querier")
			}
		}
		if g := s.Gen.CPP; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.cpp.out is required", i)
			}
			if g.Namespace == "" {
				g.Namespace = defaultCPPNamespace
			}
			resolved, err := resolveRuntime("cpp", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.CSharp; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.csharp.out is required", i)
			}
			if g.Namespace == "" {
				g.Namespace = defaultCSharpNamespace
			}
			resolved, err := resolveRuntime("csharp", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.Java; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.java.out is required", i)
			}
			if g.Package == "" {
				g.Package = defaultJavaPackage
			}
			resolved, err := resolveRuntime("java", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.Kotlin; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.kotlin.out is required", i)
			}
			if g.Package == "" {
				g.Package = defaultKotlinPackage
			}
			resolved, err := resolveRuntime("kotlin", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.TypeScript; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.typescript.out is required", i)
			}
			resolved, err := resolveRuntime("typescript", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.Rust; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.rust.out is required", i)
			}
			resolved, err := resolveRuntime("rust", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
		if g := s.Gen.PHP; g != nil {
			if g.Out == "" {
				return nil, fmt.Errorf("sql[%d].gen.php.out is required", i)
			}
			if g.Namespace == "" {
				g.Namespace = defaultPHPNamespace
			}
			resolved, err := resolveRuntime("php", g.Runtime)
			if err != nil {
				return nil, fmt.Errorf("sql[%d]: %w", i, err)
			}
			g.Runtime = resolved
		}
	}
	return &c, nil
}

func validateFunctions(sqlIndex int, functions []Function) error {
	for functionIndex, function := range functions {
		prefix := fmt.Sprintf("sql[%d].analyzer.functions[%d]", sqlIndex, functionIndex)
		if !builtins.IsFunctionIdentifier(function.Name) {
			return fmt.Errorf("%s: function name must be a non-empty YQL identifier", prefix)
		}
		if function.Returns == "" {
			return fmt.Errorf("%s: return type is required", prefix)
		}
		names := make(map[string]struct{})
		for argumentIndex, argument := range function.Args {
			if argument.Type == "" {
				return fmt.Errorf("%s: argument %d type is required", prefix, argumentIndex+1)
			}
			if argument.Name != "" && !builtins.IsParameterIdentifier(argument.Name) {
				return fmt.Errorf("%s: argument %d name must be a YQL identifier", prefix, argumentIndex+1)
			}
			if argument.Name != "" {
				if _, exists := names[argument.Name]; exists {
					return fmt.Errorf("%s: duplicate argument name %q", prefix, argument.Name)
				}
				names[argument.Name] = struct{}{}
			}
		}
	}
	return nil
}

func pluginError() error {
	return errors.New("external plugins and codegen are not supported: migrate to built-in sql[].gen generators")
}
