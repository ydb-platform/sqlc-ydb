package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestInitProfiles(t *testing.T) {
	for _, g := range Generators() {
		runtimes := append([]string{""}, g.Runtimes...)
		for alias := range g.RuntimeAliases {
			runtimes = append(runtimes, alias)
		}
		for _, runtime := range runtimes {
			t.Run(g.Language+"/"+runtime, func(t *testing.T) {
				data, err := InitYAML(g.Language, runtime)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := Parse(data)
				if err != nil {
					t.Fatalf("invalid generated YAML: %v\n%s", err, data)
				}
				minimal, err := Parse([]byte(fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  gen:\n    %s:\n      out: db\n      %s: %q\n", g.Language, g.RuntimeKey, runtime)))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(parsed, minimal) {
					t.Fatalf("full init options differ from parser defaults: %#v != %#v", parsed.SQL[0].Gen, minimal.SQL[0].Gen)
				}
			})
		}
	}
}

func TestInitDefaultSelection(t *testing.T) {
	data, err := InitYAML("", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	g := c.SQL[0].Gen
	if g.Go == nil || g.Go.Out != "db" || g.Go.SQLPackage != "ydb" || g.Python == nil || g.Python.Out != "queries" || g.Python.Runtime != "ydb" {
		t.Fatalf("legacy default selection changed: %#v", g)
	}
}

func TestInitInvalidSelection(t *testing.T) {
	for _, args := range [][2]string{{"", "ydb"}, {"javascript", ""}, {"go", "native"}, {"python", "native"}, {"java", "hibernate"}, {"csharp", "linq2db"}} {
		if _, err := InitYAML(args[0], args[1]); err == nil {
			t.Errorf("InitYAML(%q, %q) accepted an unsupported selection", args[0], args[1])
		}
	}
}

func TestUnknownOptionDefault(t *testing.T) {
	for _, language := range []string{"go", "unknown"} {
		if _, err := optionDefault(language, "missing"); err == nil {
			t.Fatalf("accepted missing option for %s", language)
		}
	}
	if runtime, ok := resolveRuntime("unknown", ""); ok || runtime != "" {
		t.Fatalf("unknown language resolved: %q, %v", runtime, ok)
	}
}

func TestInvalidGoAndPythonRuntimes(t *testing.T) {
	for _, language := range []string{"go", "python"} {
		g, err := GeneratorFor(language)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse([]byte(fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  gen:\n    %s:\n      out: db\n      %s: unsupported\n", language, g.RuntimeKey)))
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("invalid %s runtime: %v", language, err)
		}
	}
}

// Reflection is only used here to ensure adding a parser field requires documenting it.
func TestOptionCatalogCoversConfigFields(t *testing.T) {
	genType := reflect.TypeFor[Gen]()
	if len(Generators()) != genType.NumField() {
		t.Fatal("generator catalog and Gen fields differ")
	}
	for i := range genType.NumField() {
		field := genType.Field(i)
		language := field.Tag.Get("yaml")
		g, err := GeneratorFor(language)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]bool{}
		optionsType := field.Type.Elem()
		for j := range optionsType.NumField() {
			name := optionsType.Field(j).Tag.Get("yaml")
			if language == "python" && name == "package" {
				continue // This field exists solely to reject a removed option.
			}
			expected[name] = true
		}
		data, err := InitYAML(language, "")
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			SQL []struct {
				Gen map[string]map[string]any `yaml:"gen"`
			} `yaml:"sql"`
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		values := document.SQL[0].Gen[language]
		for _, option := range g.Options {
			if !expected[option.Name] || option.Description == "" || option.Default == "" || option.Type == "" {
				t.Fatalf("invalid or duplicate %s option: %#v", language, option)
			}
			delete(expected, option.Name)
			if _, ok := values[option.Name]; !ok {
				t.Errorf("%s option %s omitted from generated YAML", language, option.Name)
			}
			if !strings.Contains(string(data), "# "+option.Description) {
				t.Errorf("%s option %s lacks its explanation", language, option.Name)
			}
		}
		if len(expected) != 0 || len(values) != len(g.Options) {
			t.Fatalf("%s options missing from catalog: %v", language, expected)
		}
	}
}
