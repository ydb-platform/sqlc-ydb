// Package source resolves sqlc source paths and migration inputs.
package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/ydb-platform/sqlc-engine-ydb/internal/model"
	yql "github.com/ydb-platform/yql-parsers/go"
)

// Read accepts files, nonrecursive directories and filepath.Glob patterns.
// List order is preserved; entries in directories and glob matches are sorted.
func Read(base string, patterns []string, schema bool) ([]model.Source, error) {
	var out []model.Source
	seen := map[string]bool{}
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(base, pattern)
		}
		paths := []string{pattern}
		if strings.ContainsAny(pattern, "*?[]") {
			var err error
			paths, err = filepath.Glob(pattern)
			if err != nil {
				return nil, err
			}
			if len(paths) == 0 {
				return nil, fmt.Errorf("no files match %q", pattern)
			}
		}
		var files []string
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if !info.IsDir() {
				files = append(files, path)
				continue
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					files = append(files, filepath.Join(path, entry.Name()))
				}
			}
		}
		for _, file := range files {
			name := filepath.Base(file)
			if !strings.HasSuffix(name, ".sql") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".down.sql") {
				continue
			}
			file = filepath.Clean(file)
			if seen[file] {
				continue
			}
			seen[file] = true
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			contents := string(data)
			if schema {
				contents = upMigration(contents)
			}
			out = append(out, model.Source{Name: file, Text: contents})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no SQL files found in %v", patterns)
	}
	return out, nil
}

// Identify directives in actual comment tokens, so text inside a YQL string
// cannot accidentally truncate a migration. Offsets from ANTLR are rune offsets.
func upMigration(text string) string {
	lexer := yql.NewYQLLexer(antlr.NewInputStream(text))
	lexer.RemoveErrorListeners()
	for {
		token := lexer.NextToken()
		if token.GetTokenType() == antlr.TokenEOF {
			return text
		}
		if token.GetTokenType() != yql.YQLLexerCOMMENT {
			continue
		}
		comment := strings.ToLower(strings.TrimSpace(token.GetText()))
		for _, marker := range []string{"-- +goose down", "-- +migrate down", "---- create above / drop below ----", "-- migrate:down"} {
			if strings.HasPrefix(comment, marker) {
				return string([]rune(text)[:token.GetStart()])
			}
		}
	}
}
