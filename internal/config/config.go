// Package config loads the supported sqlc configuration contract.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"go/build/constraint"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	Package             string            `yaml:"package"`
	Out                 string            `yaml:"out"`
	SQLPackage          string            `yaml:"sql_package"`
	Rename              map[string]string `yaml:"rename"`
	Overrides           []GoOverride      `yaml:"overrides"`
	BuildTags           string            `yaml:"build_tags"`
	EmitJSONTags        bool              `yaml:"emit_json_tags"`
	JSONTagsCaseStyle   string            `yaml:"json_tags_case_style"`
	EmitInterface       bool              `yaml:"emit_interface"`
	EmitEmptySlices     bool              `yaml:"emit_empty_slices"`
	EmitExportedQueries bool              `yaml:"emit_exported_queries"`
	QueryParameterLimit *int32            `yaml:"query_parameter_limit"`
}

type GoType struct {
	Import  string `yaml:"import"`
	Package string `yaml:"package"`
	Type    string `yaml:"type"`
}

func ValidateGoBuildTags(tags string) error {
	if tags == "" {
		return nil
	}
	if strings.ContainsAny(tags, "\r\n") {
		return fmt.Errorf("gen.go.build_tags must be one Go build expression")
	}
	if _, err := constraint.Parse("//go:build " + tags); err != nil {
		return fmt.Errorf("gen.go.build_tags: %w", err)
	}
	return nil
}

func (t *GoType) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		if node.Tag != "!!str" {
			return fmt.Errorf("go_type must be a string or mapping")
		}
		t.Type = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("go_type must be a string or mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		switch node.Content[i].Value {
		case "import", "package", "type":
		default:
			return fmt.Errorf("unsupported go_type option %q", node.Content[i].Value)
		}
	}
	return node.Decode((*goTypeFields)(t))
}

type goTypeFields GoType

type GoOverride struct {
	DBType   string `yaml:"db_type"`
	Column   string `yaml:"column"`
	GoType   GoType `yaml:"go_type"`
	Nullable bool   `yaml:"nullable"`
}

func (o *GoOverride) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("override must be a mapping")
	}
	hasNullable := false
	for i := 0; i < len(node.Content); i += 2 {
		switch node.Content[i].Value {
		case "db_type", "column", "go_type":
		case "nullable":
			hasNullable = true
		case "unsigned", "go_struct_tag":
			return fmt.Errorf("unsupported override option %q", node.Content[i].Value)
		default:
			return fmt.Errorf("unknown override option %q", node.Content[i].Value)
		}
	}
	if err := node.Decode((*goOverrideFields)(o)); err != nil {
		return err
	}
	if o.Column != "" && hasNullable {
		return fmt.Errorf("nullable applies only to db_type overrides")
	}
	return nil
}

type goOverrideFields GoOverride

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
	Functions  []Function                   `yaml:"functions"`
	Parameters map[string]map[string]string `yaml:"parameters"`
	Database   *bool                        `yaml:"database"`
}

type SQL struct {
	Name     string    `yaml:"name"`
	Engine   string    `yaml:"engine"`
	Schema   Paths     `yaml:"schema"`
	Queries  Paths     `yaml:"queries"`
	Database *Database `yaml:"database"`
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
		if len(s.Queries) == 0 || (len(s.Schema) == 0 && !s.DatabaseEnabled()) {
			return nil, fmt.Errorf("sql[%d]: schema and queries paths are required", i)
		}
		if s.DatabaseEnabled() && s.Database == nil {
			return nil, fmt.Errorf("sql[%d].analyzer.database requires database.uri", i)
		}
		if s.Database != nil {
			if err := s.Database.validate(); err != nil {
				return nil, fmt.Errorf("sql[%d].%w", i, err)
			}
		}
		if err := validateFunctions(i, s.Analyzer.Functions); err != nil {
			return nil, err
		}
		if err := validateParameters(i, s.Analyzer.Parameters); err != nil {
			return nil, err
		}
		if g := s.Gen.Go; g != nil {
			if g.JSONTagsCaseStyle == "" {
				g.JSONTagsCaseStyle = "none"
			}
			switch g.JSONTagsCaseStyle {
			case "none", "camel", "pascal", "snake":
			default:
				return nil, fmt.Errorf("sql[%d].gen.go.json_tags_case_style %q must be none, camel, pascal, or snake", i, g.JSONTagsCaseStyle)
			}
			if err := ValidateGoBuildTags(g.BuildTags); err != nil {
				return nil, fmt.Errorf("sql[%d].%w", i, err)
			}
			if g.QueryParameterLimit == nil {
				limit := int32(1)
				g.QueryParameterLimit = &limit
			} else if *g.QueryParameterLimit < 0 {
				return nil, fmt.Errorf("sql[%d].gen.go.query_parameter_limit must not be negative", i)
			}
			if len(g.Rename) == 0 {
				g.Rename = nil
			}
			if len(g.Overrides) == 0 {
				g.Overrides = nil
			}
			for j, override := range g.Overrides {
				where := fmt.Sprintf("sql[%d].gen.go.overrides[%d]", i, j)
				if (override.DBType == "") == (override.Column == "") {
					return nil, fmt.Errorf("%s: specify exactly one of db_type or column", where)
				}
				if override.GoType.Type == "" {
					return nil, fmt.Errorf("%s.go_type is required", where)
				}
				if override.Column != "" && override.Nullable {
					return nil, fmt.Errorf("%s: nullable applies only to db_type overrides", where)
				}
			}
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

func validateParameters(sqlIndex int, queries map[string]map[string]string) error {
	queryNames := make([]string, 0, len(queries))
	for query := range queries {
		queryNames = append(queryNames, query)
	}
	sort.Strings(queryNames)
	for _, query := range queryNames {
		parameters := queries[query]
		if query == "" {
			return fmt.Errorf("sql[%d].analyzer.parameters: query name is required", sqlIndex)
		}
		if len(parameters) == 0 {
			return fmt.Errorf("sql[%d].analyzer.parameters.%s: at least one parameter is required", sqlIndex, query)
		}
		names := make([]string, 0, len(parameters))
		for name := range parameters {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			typ := parameters[name]
			if name == "" || strings.HasPrefix(name, "$") {
				return fmt.Errorf("sql[%d].analyzer.parameters.%s: parameter name %q must be non-empty and omit the leading $", sqlIndex, query, name)
			}
			if typ == "" {
				return fmt.Errorf("sql[%d].analyzer.parameters.%s.%s: type is required", sqlIndex, query, name)
			}
		}
	}
	return nil
}

func pluginError() error {
	return errors.New("external plugins and codegen are not supported: migrate to built-in sql[].gen generators")
}
