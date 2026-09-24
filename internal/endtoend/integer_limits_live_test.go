package endtoend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

// Exercise server typing and generated bindings separately: preserving a signed
// parameter is observable for negative values, not just in generated source.
func TestLiveYDBIntegerLimits(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for integer alias and pagination validation")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_integer_limits_%d", time.Now().UnixNano())
	schema := "CREATE TABLE " + table + " (id Uint64 NOT NULL, tiny TinyInt NOT NULL, small SmallInt NOT NULL, regular Int NOT NULL, other Integer NOT NULL, big BigInt NOT NULL, PRIMARY KEY(id));"
	var queries strings.Builder
	queries.WriteString(`-- name: EchoAliases :one
DECLARE $tiny AS TinyInt;
DECLARE $small AS SmallInt;
DECLARE $regular AS Int;
DECLARE $other AS Integer;
DECLARE $big AS BigInt;
SELECT $tiny AS tiny, $small AS small, $regular AS regular, $other AS other, $big AS big;

`)
	widths := []string{"Int8", "Int16", "Int32", "Uint8", "Uint16", "Uint32", "Uint64"}
	for _, width := range widths {
		for _, optional := range []bool{false, true} {
			name, typ := width, width
			if optional {
				name, typ = "Optional"+width, "Optional<"+width+">"
			}
			fmt.Fprintf(&queries, "-- name: Read%s :many\nDECLARE $page_size AS %s;\nDECLARE $page_offset AS %s;\nSELECT id FROM %s ORDER BY id LIMIT $page_size OFFSET $page_offset;\n\n", name, typ, typ, table)
		}
	}
	fmt.Fprintf(&queries, "-- name: ReadNull :many\nSELECT id FROM %s ORDER BY id LIMIT NULL OFFSET NULL;\n\n", table)
	fmt.Fprintf(&queries, "-- name: ReadLiteralInt64 :many\nSELECT id FROM %s ORDER BY id LIMIT 1l OFFSET 1l;\n\n", table)
	fmt.Fprintf(&queries, "-- name: ReadMaxLiteralInt64 :many\nSELECT id FROM %s ORDER BY id LIMIT 9223372036854775807l;\n\n", table)
	fmt.Fprintf(&queries, "-- name: ReadComma :many\nDECLARE $page_size AS Int;\nDECLARE $page_offset AS Uint32;\nSELECT id FROM %s ORDER BY id LIMIT $page_offset, $page_size;\n", table)
	config := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		config += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, contents := range map[string]string{
		"schema.sql": schema, "queries.sql": queries.String(), "sqlc.yaml": config,
		"go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600))
	}
	var stdout, stderr bytes.Buffer
	require.Zero(t, cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr), "generate integer fixture: %s", stderr.String())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup, sqlImport := "q := New(driver.Query())", ""
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver))\n\tdefer db.Close()\n\tq := New(db)"
				sqlImport = "\"database/sql\""
			}
			source := integerLimitsRuntimeSource(dsn, table, schema, setup, sqlImport, widths)
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, false)
		})
	}
}

func integerLimitsRuntimeSource(dsn, table, schema, setup, sqlImport string, widths []string) string {
	var checks strings.Builder
	for _, width := range widths {
		goType := strings.ToLower(width)
		fmt.Fprintf(&checks, `
	t.Run(%q, func(t *testing.T) {
		rows, err := q.Read%s(ctx, Read%sParams{PageSize: %s(1), PageOffset: %s(1)})
		if err != nil || len(rows) != 1 || rows[0].ID != 2 {
			t.Fatalf("page: %%v %%v", rows, err)
		}
		rows, err = q.Read%s(ctx, Read%sParams{PageSize: 0, PageOffset: 0})
		if err != nil || len(rows) != 0 {
			t.Fatalf("zero limit: %%v %%v", rows, err)
		}
`, width, width, width, goType, goType, width, width)
		if strings.HasPrefix(width, "Int") {
			fmt.Fprintf(&checks, `
		rows, err = q.Read%s(ctx, Read%sParams{PageSize: -1, PageOffset: 0})
		if err != nil || len(rows) != 3 {
			t.Fatalf("negative limit follows server semantics: %%v %%v", rows, err)
		}
		rows, err = q.Read%s(ctx, Read%sParams{PageSize: 1, PageOffset: -1})
		if err != nil || len(rows) != 0 {
			t.Fatalf("negative offset follows server semantics: %%v %%v", rows, err)
		}
`, width, width, width, width)
		}
		fmt.Fprintf(&checks, `
		one := %s(1)
		optional, err := q.ReadOptional%s(ctx, ReadOptional%sParams{PageSize: &one, PageOffset: &one})
		if err != nil || len(optional) != 1 || optional[0].ID != 2 {
			t.Fatalf("optional page: %%v %%v", optional, err)
		}
		optional, err = q.ReadOptional%s(ctx, ReadOptional%sParams{})
		if err != nil || len(optional) != 3 {
			t.Fatalf("NULL limit and offset: %%v %%v", optional, err)
		}
	})
`, goType, width, width, width, width)
	}
	return `package records

import (
	"context"
	` + sqlImport + `
	"strings"
	"testing"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
)

func TestIntegerLimits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	driver, err := ydb.Open(ctx, ` + strconv.Quote(dsn) + `, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	table := ` + strconv.Quote(table) + `
	if err := driver.Query().Exec(ctx, ` + strconv.Quote(schema) + `); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := driver.Query().Exec(cleanup, "DROP TABLE "+table); err != nil {
			t.Error(err)
		}
	}()
	if err := driver.Query().Exec(ctx, "UPSERT INTO "+table+" (id,tiny,small,regular,other,big) VALUES (1ul,1t,2s,3,4,5l),(2ul,1t,2s,3,4,5l),(3ul,1t,2s,3,4,5l);"); err != nil {
		t.Fatal(err)
	}
	` + setup + `
	aliases, err := q.EchoAliases(ctx, EchoAliasesParams{Tiny: int8(-128), Small: int16(-32768), Regular: int32(-2147483648), Other: int32(2147483647), Big: int64(9223372036854775807)})
	if err != nil || aliases.Tiny != -128 || aliases.Small != -32768 || aliases.Regular != -2147483648 || aliases.Other != 2147483647 || aliases.Big != 9223372036854775807 {
		t.Fatalf("alias bindings: %+v %v", aliases, err)
	}
	metadata, err := driver.Query().Query(ctx, "SELECT tiny,small,regular,other,big FROM "+table+" LIMIT 0;")
	if err != nil {
		t.Fatal(err)
	}
	set, err := metadata.NextResultSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"Int8", "Int16", "Int32", "Int32", "Int64"} {
		if got := set.ColumnTypes()[i].Yql(); got != want {
			t.Errorf("alias column %d: got %s, want %s", i, got, want)
		}
	}
	if err := metadata.Close(ctx); err != nil {
		t.Fatal(err)
	}
` + checks.String() + `
	all, err := q.ReadNull(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("literal NULL pagination: %v %v", all, err)
	}
	literal, err := q.ReadLiteralInt64(ctx)
	if err != nil || len(literal) != 1 || literal[0].ID != 2 {
		t.Fatalf("positive Int64 literals: %v %v", literal, err)
	}
	largestLiteral, err := q.ReadMaxLiteralInt64(ctx)
	if err != nil || len(largestLiteral) != 3 {
		t.Fatalf("maximum Int64 literal: %v %v", largestLiteral, err)
	}
	comma, err := q.ReadComma(ctx, ReadCommaParams{PageOffset: uint32(1), PageSize: int32(2)})
	if err != nil || len(comma) != 2 || comma[0].ID != 2 || comma[1].ID != 3 {
		t.Fatalf("comma limit order: %v %v", comma, err)
	}
	maximum, err := q.ReadUint64(ctx, ReadUint64Params{PageSize: ^uint64(0)})
	if err != nil || len(maximum) != 3 {
		t.Fatalf("maximum Uint64 limit: %v %v", maximum, err)
	}
	maximum, err = q.ReadUint64(ctx, ReadUint64Params{PageSize: 1, PageOffset: ^uint64(0)})
	if err != nil || len(maximum) != 0 {
		t.Fatalf("maximum Uint64 offset: %v %v", maximum, err)
	}
	for _, tc := range []struct {
		name, declaration, clause, diagnostic string
		params query.ExecuteOption
	}{
		{"int64 limit", "DECLARE $n AS Int64;", "LIMIT $n", "Failed to convert type: Int64 to Uint64", query.WithParameters(ydb.ParamsBuilder().Param("$n").Int64(1).Build())},
		{"int64 offset", "DECLARE $n AS Int64;", "LIMIT 1ul OFFSET $n", "Failed to convert type: Int64 to Uint64", query.WithParameters(ydb.ParamsBuilder().Param("$n").Int64(1).Build())},
		{"double", "DECLARE $n AS Double;", "LIMIT $n", "Failed to convert type: Double to Uint64", query.WithParameters(ydb.ParamsBuilder().Param("$n").Double(1.5).Build())},
		{"bool", "DECLARE $n AS Bool;", "LIMIT $n", "coalesce types", query.WithParameters(ydb.ParamsBuilder().Param("$n").Bool(true).Build())},
		{"utf8", "DECLARE $n AS Utf8;", "LIMIT 1ul OFFSET $n", "coalesce types", query.WithParameters(ydb.ParamsBuilder().Param("$n").Text("1").Build())},
		{"cast int64", "", "LIMIT CAST(1 AS Int64)", "Failed to convert type: Int64 to Uint64", nil},
		{"optional int64", "", "LIMIT CAST(NULL AS Int64?)", "Failed to convert type", nil},
		{"overflow", "", "LIMIT 18446744073709551616ul", "overflow", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := tc.declaration+"SELECT id FROM "+table+" ORDER BY id "+tc.clause+";"
			err := driver.Query().Exec(ctx, sql, query.WithExecMode(query.ExecModeExplain))
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("EXPLAIN should reject unsupported pagination: %v", err)
			}
			var opts []query.ExecuteOption
			if tc.params != nil {
				opts = append(opts, tc.params)
			}
			err = driver.Query().Exec(ctx, sql, opts...)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("execution should reject unsupported pagination: %v", err)
			}
		})
	}
}
`
}
