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
		name     string
		generate func() ([]model.File, error)
		want     string
	}{
		{"go-ydb", func() ([]model.File, error) { return golang.Generate(analysis, golang.Options{Runtime: "ydb"}) }, "q.db.Exec("},
		{"go-sql", func() ([]model.File, error) {
			return golang.Generate(analysis, golang.Options{Runtime: "database/sql"})
		}, "for rows.Next() {"},
		{"cpp-ydb", func() ([]model.File, error) { return cpp.Generate(analysis, cpp.Options{Runtime: "ydb"}) }, "ThrowOnError(sqlc_status)"},
		{"cpp-userver", func() ([]model.File, error) { return cpp.Generate(analysis, cpp.Options{Runtime: "userver"}) }, "static_cast<void>("},
		{"csharp-adonet", func() ([]model.File, error) { return csharp.Generate(analysis, csharp.Options{Runtime: "adonet"}) }, "ExecuteNonQueryAsync("},
		{"csharp-dapper", func() ([]model.File, error) { return csharp.Generate(analysis, csharp.Options{Runtime: "dapper"}) }, "ExecuteAsync(command)"},
		{"java-ydb", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "ydb"}) }, ".execute().join().getStatus().expectSuccess()"},
		{"java-jdbc", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "jdbc"}) }, "_prepared.getMoreResults()"},
		{"java-jooq", func() ([]model.File, error) { return java.Generate(analysis, java.Options{Runtime: "jooq"}) }, "_prepared.getMoreResults()"},
		{"kotlin-ydb", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "ydb"}) }, ".execute().join().getStatus().expectSuccess()"},
		{"kotlin-jdbc", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "jdbc"}) }, "_prepared.moreResults"},
		{"kotlin-exposed", func() ([]model.File, error) { return kotlin.Generate(analysis, kotlin.Options{Runtime: "exposed"}) }, "_prepared.moreResults"},
		{"php-ydb", func() ([]model.File, error) { return php.Generate(analysis, php.Options{Runtime: "ydb"}) }, "return (new YdbRawExecutor($this->table))->execute"},
		{"python-ydb", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "ydb"}) }, "result_sets = self._execute("},
		{"python-dbapi", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "dbapi"}) }, "cursor.execute("},
		{"python-sqlalchemy", func() ([]model.File, error) { return python.Generate(analysis, python.Options{Runtime: "sqlalchemy"}) }, "self._connection.execute("},
		{"rust-ydb", func() ([]model.File, error) { return rust.Generate(analysis, rust.Options{Runtime: "ydb"}) }, ".exec("},
		{"typescript-ydb", func() ([]model.File, error) { return typescript.Generate(analysis, typescript.Options{Runtime: "ydb"}) }, "await stmt;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := tc.generate()
			require.NoError(t, err)
			require.NotEmpty(t, files)
			var generated []byte
			for _, file := range files {
				generated = append(generated, file.Content...)
			}
			require.Contains(t, string(generated), tc.want)
		})
	}
}
