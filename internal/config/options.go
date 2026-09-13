package config

//go:generate go run ./gendocs -file ../../docs/targets.md

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	defaultCPPNamespace    = "db"
	defaultCSharpNamespace = "Db"
	defaultJavaPackage     = "db"
	defaultKotlinPackage   = "db"
	defaultPHPNamespace    = "Db"
	defaultSyncQuerier     = true
)

// Option describes one supported key inside sql[].gen.<language>.
type Option struct {
	Name        string
	Type        string
	Default     string
	Description string
	Required    bool
	Values      []string
}

// Generator describes a built-in generator's configuration, including runtime aliases.
type Generator struct {
	Language       string
	RuntimeKey     string
	DefaultRuntime string
	Runtimes       []string
	RuntimeAliases map[string]string
	Options        []Option
}

// Generators returns the configuration contract used by init and its reference documentation.
func Generators() []Generator {
	out := Option{Name: "out", Type: "string", Default: "required", Required: true, Description: "Output directory, relative to sqlc.yaml."}
	return []Generator{
		generator("go", "sql_package", "database/sql", []string{"database/sql", "ydb"}, false,
			out,
			Option{Name: "package", Type: "string", Default: "basename(out)", Description: "Generated Go package name; defaults to the output directory's base name."},
			Option{Name: "emit_json_tags", Type: "boolean", Default: "false", Description: "Add JSON tags to generated struct fields."},
			Option{Name: "emit_interface", Type: "boolean", Default: "false", Description: "Generate the Querier interface implemented by Queries."},
			Option{Name: "emit_empty_slices", Type: "boolean", Default: "false", Description: "Return empty slices instead of nil for successful :many queries with no rows."}),
		generator("python", "runtime", "ydb", []string{"ydb", "dbapi", "sqlalchemy"}, false,
			out,
			Option{Name: "emit_sync_querier", Type: "boolean", Default: strconv.FormatBool(defaultSyncQuerier), Description: "Generate synchronous query helpers; must remain true while asynchronous generation is unsupported."},
			Option{Name: "emit_async_querier", Type: "boolean", Default: "false", Description: "Asynchronous generation is unsupported; true produces a generation error."}),
		generator("cpp", "runtime", "ydb", []string{"ydb", "userver"}, true, out,
			Option{Name: "namespace", Type: "string", Default: defaultCPPNamespace, Description: "Namespace containing the generated C++ types and helpers."}),
		generator("csharp", "runtime", "adonet", []string{"adonet", "dapper"}, false, out,
			Option{Name: "namespace", Type: "string", Default: defaultCSharpNamespace, Description: "Namespace containing the generated C# types and helpers."}),
		generator("java", "runtime", "ydb", []string{"ydb", "jdbc", "jooq"}, true, out,
			Option{Name: "package", Type: "string", Default: defaultJavaPackage, Description: "Package containing the generated Java types and helpers."}),
		generator("kotlin", "runtime", "ydb", []string{"ydb", "jdbc", "exposed"}, true, out,
			Option{Name: "package", Type: "string", Default: defaultKotlinPackage, Description: "Package containing the generated Kotlin types and helpers."}),
		generator("typescript", "runtime", "ydb", []string{"ydb"}, false, out),
		generator("rust", "runtime", "ydb", []string{"ydb"}, false, out),
		generator("php", "runtime", "ydb", []string{"ydb"}, false, out,
			Option{Name: "namespace", Type: "string", Default: defaultPHPNamespace, Description: "Namespace containing the generated PHP types and helpers."}),
	}
}

func generator(language, runtimeKey, defaultRuntime string, runtimes []string, nativeAlias bool, options ...Option) Generator {
	g := Generator{Language: language, RuntimeKey: runtimeKey, DefaultRuntime: defaultRuntime, Runtimes: runtimes}
	description := "SDK or framework used by the generated helpers."
	values := slices.Clone(runtimes)
	if nativeAlias {
		g.RuntimeAliases = map[string]string{"native": "ydb"}
		values = append(values, "native")
		description += " native is an alias for ydb."
	}
	g.Options = append(options, Option{Name: runtimeKey, Type: "enum", Default: defaultRuntime, Description: description, Values: values})
	return g
}

func GeneratorFor(language string) (Generator, error) {
	for _, g := range Generators() {
		if g.Language == language {
			return g, nil
		}
	}
	return Generator{}, fmt.Errorf("unsupported language %q; use sqlc-ydb init --help to list supported languages", language)
}

func resolveRuntime(language, runtime string) (string, error) {
	g, err := GeneratorFor(language)
	if err != nil {
		return "", err
	}
	return g.ResolveRuntime(runtime)
}

func (g Generator) ResolveRuntime(runtime string) (string, error) {
	if runtime == "" {
		return g.DefaultRuntime, nil
	}
	if canonical, ok := g.RuntimeAliases[runtime]; ok {
		return canonical, nil
	}
	if slices.Contains(g.Runtimes, runtime) {
		return runtime, nil
	}
	return "", fmt.Errorf("unsupported %s %s %q (use %s)", g.Language, g.RuntimeKey, runtime, strings.Join(g.Runtimes, ", "))
}

// InitProfile contains a validated generator selection and its starter output directory.
type InitProfile struct {
	Generator Generator
	Runtime   string
	Out       string
}

func InitProfiles(language, runtime string) ([]InitProfile, error) {
	if language == "" && runtime != "" {
		return nil, fmt.Errorf("--runtime requires --language")
	}
	selected := []string{language}
	if language == "" {
		selected = []string{"go", "python"}
		runtime = "ydb"
	}
	profiles := make([]InitProfile, 0, len(selected))
	for _, name := range selected {
		g, err := GeneratorFor(name)
		if err != nil {
			return nil, err
		}
		resolved, err := g.ResolveRuntime(runtime)
		if err != nil {
			return nil, err
		}
		out := "db"
		if language == "" && name == "python" {
			out = "queries"
		}
		profiles = append(profiles, InitProfile{Generator: g, Runtime: resolved, Out: out})
	}
	return profiles, nil
}

// InitYAML renders complete, commented configurations for validated profiles.
func InitYAML(profiles []InitProfile) ([]byte, error) {
	gen := &yaml.Node{Kind: yaml.MappingNode}
	for _, profile := range profiles {
		g := profile.Generator
		options := &yaml.Node{Kind: yaml.MappingNode}
		for _, option := range g.Options {
			value := option.Default
			switch option.Name {
			case g.RuntimeKey:
				value = profile.Runtime
			case "out":
				value = profile.Out
			case "package":
				if g.Language == "go" {
					value = "db"
				}
			}
			key := scalar(option.Name)
			key.HeadComment = option.Description
			node := scalar(value)
			if option.Type == "boolean" {
				node.Tag = "!!bool"
			}
			options.Content = append(options.Content, key, node)
		}
		gen.Content = append(gen.Content, scalar(g.Language), options)
	}
	querySet := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("engine"), scalar("ydb"),
		scalar("schema"), scalar("schema.sql"),
		scalar("queries"), scalar("query.sql"),
		scalar("gen"), gen,
	}}
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("version"), scalar("2"),
		scalar("sql"), {Kind: yaml.SequenceNode, Content: []*yaml.Node{querySet}},
	}}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
