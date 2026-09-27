package endtoend

import (
	"testing"

	"github.com/stretchr/testify/require"
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
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestExecDiscardGenerationByRuntime(t *testing.T) {
	analysis, err := analyzer.Analyze(
		[]model.Source{{Name: "schema.sql", Text: "CREATE TABLE records (id Uint64 NOT NULL, PRIMARY KEY(id));"}},
		[]model.Source{{Name: "queries.sql", Text: "-- name: DiscardRows :exec\nDECLARE $id AS Uint64;\nSELECT id FROM records WHERE id=$id;\nDELETE FROM records WHERE id=$id;"}},
	)
	require.NoError(t, err)
	for _, tc := range []struct {
		name        string
		generate    func() ([]model.File, error)
		unsupported string
	}{
		{"go-ydb", func() ([]model.File, error) { return golang.Generate(analysis, golang.Options{Runtime: "ydb"}) }, ""},
		{"go-sql", func() ([]model.File, error) {
			return golang.Generate(analysis, golang.Options{Runtime: "database/sql"})
		}, ""},
		{"cpp-ydb", func() ([]model.File, error) { return cpp.Generate(analysis, cpp.Options{Runtime: "ydb"}) }, ""},
		{"cpp-userver", func() ([]model.File, error) { return cpp.Generate(analysis, cpp.Options{Runtime: "userver"}) }, ""},
		{"csharp-adonet", func() ([]model.File, error) { return csharp.Generate(analysis, csharp.Options{Runtime: "adonet"}) }, ""},
		{"csharp-dapper", func() ([]model.File, error) { return csharp.Generate(analysis, csharp.Options{Runtime: "dapper"}) }, ""},
		{"java-ydb", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "ydb"}) }, ""},
		{"java-jdbc", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "jdbc"}) }, ""},
		{"java-jooq", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "jooq"}) }, ""},
		{"kotlin-ydb", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "ydb"}) }, ""},
		{"kotlin-jdbc", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "jdbc"}) }, ""},
		{"kotlin-exposed", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "exposed"}) }, ""},
		{"php-ydb", func() ([]model.File, error) { return php.Generate(analysis, php.Options{Runtime: "ydb"}) }, ""},
		{"python-ydb", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "ydb"}) }, ""},
		{"python-dbapi", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "dbapi"}) }, ""},
		{"python-sqlalchemy", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "sqlalchemy"}) }, ""},
		{"rust-ydb", func() ([]model.File, error) { return rust.Generate(analysis, rust.Options{Runtime: "ydb"}) }, ""},
		{"typescript-ydb", func() ([]model.File, error) { return typescript.Generate(analysis, typescript.Options{Runtime: "ydb"}) }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := tc.generate()
			if tc.unsupported != "" {
				require.ErrorContains(t, err, tc.unsupported)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, files)
		})
	}
}
