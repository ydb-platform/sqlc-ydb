package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

func initHelp(a arguments) string {
	var b strings.Builder
	b.WriteString(`Create a version 2 sqlc.yaml with every generator option and explanatory comments.
Existing files are never overwritten. With no language, configure Go (ydb) and Python (ydb).

Usage:
  sqlc-ydb init [--language <language>] [--runtime <runtime>] [-f sqlc.yaml]

Options:
  --language <language>  Select a generator (listed below)
  --runtime <runtime>    Select its framework; requires --language
  --all-options          Include every option (the default; accepted explicitly)
  -f, --file <path>      Write an alternate configuration file; use --file=-name or ./-name for leading dashes
  --v2                  Use config version 2 (the default)
  -h, --help             Show help; select a language for its YAML options

Languages and runtimes:
`)
	for _, generator := range config.Generators() {
		fmt.Fprintf(&b, "  %-12s %s (default: %s)\n", generator.Language, strings.Join(generator.Runtimes, ", "), generator.DefaultRuntime)
		aliases := make([]string, 0, len(generator.RuntimeAliases))
		for alias := range generator.RuntimeAliases {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			fmt.Fprintf(&b, "               %s is an alias for %s\n", alias, generator.RuntimeAliases[alias])
		}
	}
	if a.language == "" {
		b.WriteString("\nExample: sqlc-ydb init --language go --runtime ydb --help\n")
		return b.String()
	}
	profile := a.initProfiles[0]
	generator, runtime := profile.Generator, profile.Runtime
	fmt.Fprintf(&b, "\nYAML options for gen.%s (runtime: %s):\n", generator.Language, runtime)
	b.WriteString("Edit these fields in sqlc.yaml after init; they are not command-line flags.\n")
	for _, option := range generator.Options {
		if option.Required {
			fmt.Fprintf(&b, "\n  %s (%s; required)", option.Name, option.Type)
		} else {
			fmt.Fprintf(&b, "\n  %s (%s; default: %s)", option.Name, option.Type, option.Default)
		}
		fmt.Fprintf(&b, "\n    %s\n", option.Description)
		if len(option.Values) > 0 {
			fmt.Fprintf(&b, "    Values: %s\n", strings.Join(option.Values, ", "))
		}
		if option.Name == generator.RuntimeKey {
			fmt.Fprintf(&b, "    Selected for this configuration: %s\n", runtime)
		}
	}
	return b.String()
}
