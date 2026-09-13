// Command gendocs updates the generator option tables in docs/targets.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

const (
	startMarker = "<!-- BEGIN GENERATED GENERATOR OPTIONS -->"
	endMarker   = "<!-- END GENERATED GENERATOR OPTIONS -->"
)

func main() {
	path := flag.String("file", "docs/targets.md", "Target reference to update")
	flag.Parse()
	if err := update(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func update(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := replaceSection(string(data))
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0644)
}

func replaceSection(text string) (string, error) {
	if strings.Count(text, startMarker) != 1 || strings.Count(text, endMarker) != 1 {
		return "", fmt.Errorf("expected one generator options section in the target reference")
	}
	start := strings.Index(text, startMarker) + len(startMarker)
	end := strings.Index(text, endMarker)
	if end < start {
		return "", fmt.Errorf("generator options section markers are reversed")
	}
	var b strings.Builder
	b.WriteString("\n\n")
	for _, generator := range config.Generators() {
		fmt.Fprintf(&b, "### gen.%s\n\n", generator.Language)
		b.WriteString("| Option | Type | Default | Description |\n| --- | --- | --- | --- |\n")
		for _, option := range generator.Options {
			defaultValue := "`" + option.Default + "`"
			if option.Required {
				defaultValue = "Required"
			}
			description := option.Description
			if len(option.Values) > 0 {
				description += " Values: `" + strings.Join(option.Values, "`, `") + "`."
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", option.Name, option.Type, defaultValue, strings.ReplaceAll(description, "|", "\\|"))
		}
		b.WriteString("\n")
	}
	return text[:start] + b.String() + text[end:], nil
}
