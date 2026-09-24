package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

func TestExplicitBatchDeclarationsSurviveGeneration(t *testing.T) {
	const declaration = "DECLARE $books AS List<Struct<\n    book_id: Uint64,\n    data: Json\n>>;"
	for _, profile := range []struct{ language, runtime, key string }{
		{"go", "ydb", "sql_package"}, {"go", "database/sql", "sql_package"},
		{"python", "ydb", "runtime"}, {"python", "dbapi", "runtime"}, {"python", "sqlalchemy", "runtime"},
		{"cpp", "ydb", "runtime"}, {"cpp", "userver", "runtime"},
		{"csharp", "adonet", "runtime"}, {"csharp", "dapper", "runtime"},
		{"rust", "ydb", "runtime"}, {"php", "ydb", "runtime"},
		{"typescript", "ydb", "runtime"},
		{"java", "ydb", "runtime"}, {"java", "jdbc", "runtime"}, {"java", "jooq", "runtime"},
		{"kotlin", "ydb", "runtime"}, {"kotlin", "jdbc", "runtime"}, {"kotlin", "exposed", "runtime"},
	} {
		t.Run(profile.language+"/"+profile.runtime, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "schema.sql"), []byte("CREATE TABLE books (book_id Uint64 NOT NULL, data Json NOT NULL, PRIMARY KEY(book_id));"))
			write(t, filepath.Join(dir, "queries.sql"), []byte("-- name: CreateBooks :exec\n"+declaration+"\nINSERT INTO books (book_id,data) SELECT book_id,data FROM AS_TABLE($books);"))
			config := fmt.Sprintf("version: \"2\"\nsql:\n  - engine: ydb\n    schema: schema.sql\n    queries: queries.sql\n    gen:\n      %s:\n        out: generated\n        %s: %s\n", profile.language, profile.key, profile.runtime)
			write(t, filepath.Join(dir, "sqlc.yaml"), []byte(config))
			var out, stderr bytes.Buffer
			status := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &out, &stderr)
			require.Zero(t, status, "generate failed: %s", stderr.String())
			entries, err := os.ReadDir(filepath.Join(dir, "generated"))
			require.NoError(t, err)
			wantDeclaration := declaration
			if profile.language == "python" && profile.runtime == "sqlalchemy" {
				wantDeclaration = strings.ReplaceAll(declaration, ":", "\\\\:")
			}
			if profile.language == "kotlin" {
				wantDeclaration = strings.ReplaceAll(declaration, "$", `\$`)
			}
			preserved := false
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				content, err := os.ReadFile(filepath.Join(dir, "generated", entry.Name()))
				require.NoError(t, err)
				remaining := string(content)
				complete := true
				for _, line := range strings.Split(wantDeclaration, "\n") {
					_, rest, found := strings.Cut(remaining, line)
					if !found {
						complete = false
						break
					}
					remaining = rest
				}
				preserved = preserved || complete
			}
			require.True(t, preserved, "%s/%s removed or changed the explicit batch DECLARE", profile.language, profile.runtime)
		})
	}
}
