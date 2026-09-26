package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				data, err := initYAML(g.Language, runtime)
				require.NoError(t, err)
				parsed, err := Parse(data)
				require.NoError(t, err, "invalid generated YAML: %v\n%s", err, data)
				minimal, err := Parse([]byte(fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  gen:\n    %s:\n      out: db\n      %s: %q\n", g.Language, g.RuntimeKey, runtime)))
				require.NoError(t, err)
				require.Equal(t, minimal, parsed, "full init options differ from parser defaults")
			})
		}
	}
}

func TestInitDefaultSelection(t *testing.T) {
	data, err := initYAML("", "")
	require.NoError(t, err)
	c, err := Parse(data)
	require.NoError(t, err)
	g := c.SQL[0].Gen
	require.False(t, g.Go == nil || g.Go.Out != "db" || g.Go.SQLPackage != "ydb" || g.Python == nil || g.Python.Out != "queries" || g.Python.Runtime != "ydb", "legacy default selection changed: %#v", g)
}

func TestInitGoRenameIsAnEmptyMapping(t *testing.T) {
	data, err := initYAML("go", "ydb")
	require.NoError(t, err)
	require.Contains(t, string(data), "rename: {}")
	c, err := Parse(data)
	require.NoError(t, err)
	require.Nil(t, c.SQL[0].Gen.Go.Rename)
}

func TestInitInvalidSelection(t *testing.T) {
	for _, args := range [][2]string{{"", "ydb"}, {"javascript", ""}, {"go", "native"}, {"python", "native"}, {"java", "hibernate"}, {"csharp", "linq2db"}} {
		_, err := initYAML(args[0], args[1])
		assert.Error(t, err, "initYAML(%q, %q) accepted an unsupported selection", args[0], args[1])
	}
}

func TestUnknownRuntimeLanguage(t *testing.T) {
	runtime, err := resolveRuntime("unknown", "")
	require.Error(t, err)
	require.Empty(t, runtime)
}

func TestRuntimeErrorsUseCatalog(t *testing.T) {
	for _, g := range Generators() {
		_, err := Parse([]byte(fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  gen:\n    %s:\n      out: db\n      %s: unsupported\n", g.Language, g.RuntimeKey)))
		assert.False(t, err == nil || !strings.Contains(err.Error(), "sql[0]:") || !strings.Contains(err.Error(), "unsupported "+g.Language+" "+g.RuntimeKey) || !strings.Contains(err.Error(), "use "+strings.Join(g.Runtimes, ", ")), "%s runtime error does not describe catalog values: %v", g.Language, err)
	}
}

// Reflection is only used here to ensure adding a parser field requires documenting it.
func TestOptionCatalogCoversConfigFields(t *testing.T) {
	genType := reflect.TypeFor[Gen]()
	require.Equal(t, genType.NumField(), len(Generators()), "generator catalog and Gen fields differ")
	for i := range genType.NumField() {
		field := genType.Field(i)
		language := field.Tag.Get("yaml")
		g, err := GeneratorFor(language)
		require.NoError(t, err)
		expected := map[string]bool{}
		optionsType := field.Type.Elem()
		for j := range optionsType.NumField() {
			name := optionsType.Field(j).Tag.Get("yaml")
			if language == "python" && name == "package" {
				continue // This field exists solely to reject a removed option.
			}
			expected[name] = true
		}
		data, err := initYAML(language, "")
		require.NoError(t, err)
		var document struct {
			SQL []struct {
				Gen map[string]map[string]any `yaml:"gen"`
			} `yaml:"sql"`
		}
		require.NoError(t, yaml.Unmarshal(data, &document))
		values := document.SQL[0].Gen[language]
		for _, option := range g.Options {
			require.False(t, !expected[option.Name] || option.Description == "" || option.Default == "" || option.Type == "", "invalid or duplicate %s option: %#v", language, option)
			delete(expected, option.Name)
			assert.Contains(t, values, option.Name, "%s option omitted from generated YAML", language)
			assert.True(t, strings.Contains(string(data), "# "+option.Description), "%s option %s lacks its explanation", language, option.Name)
		}
		require.Empty(t, expected, "%s options missing from catalog", language)
		require.Len(t, values, len(g.Options), "%s option count", language)
	}
}

func initYAML(language, runtime string) ([]byte, error) {
	profiles, err := InitProfiles(language, runtime)
	if err != nil {
		return nil, err
	}
	return InitYAML(profiles)
}
