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

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/cli"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestLiveYDBSharedExpressions(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for shared expression validation")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_expressions_%d", time.Now().UnixNano())
	schema := "CREATE TABLE " + table + " (id Uint64 NOT NULL, enabled Bool NOT NULL, flag Bool, counter Uint32, text String, PRIMARY KEY(id));"
	collisionTable := table + "_collision"
	collisionSchema := "CREATE TABLE " + collisionTable + " (za Utf8 NOT NULL, zb Uint64 NOT NULL, PRIMARY KEY(zb));"
	combinedSchema := schema + "\n" + collisionSchema
	queries := strings.ReplaceAll(sharedExpressionsQueries, "records", table)
	analysis, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: combinedSchema}}, []model.Source{{Name: "queries.sql", Text: queries}})
	if err != nil {
		t.Fatal(err)
	}
	configuration := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		configuration += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, contents := range map[string]string{"schema.sql": combinedSchema, "queries.sql": queries, "sqlc.yaml": configuration, "go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate shared expressions: %s", stderr.String())
	}
	// Timezone types are analyzed and compared with server metadata separately;
	// their Go row representation is outside the generated scalar subset.
	timezones, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: "timezone.sql", Text: "-- name: Timezones :many\nSELECT CurrentTzDate('Europe/Moscow') AS day, CurrentTzDatetime(CAST('Europe/Moscow' AS String?)) AS seconds, CurrentTzTimestamp('invalid-zone') AS micros FROM " + table + " LIMIT 0;"}})
	if err != nil {
		t.Fatal(err)
	}
	analysis.Queries = append(analysis.Queries, timezones.Queries...)
	var metadata strings.Builder
	var collisionSQL string
	for _, query := range analysis.Queries {
		if query.Name == "WildcardCollision" {
			collisionSQL = query.SQL
		}
		fmt.Fprintf(&metadata, "\n checkMetadata(t,ctx,driver,%q,[]string{", query.SQL)
		for _, column := range query.ResultSets[0].Columns {
			fmt.Fprintf(&metadata, "%q,", column.ResultName())
		}
		metadata.WriteString("},[]string{")
		for _, column := range query.ResultSets[0].Columns {
			fmt.Fprintf(&metadata, "%q,", column.Type.String())
		}
		metadata.WriteString("})\n")
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup, sqlImport := "q := New(driver.Query())", ""
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver)); defer db.Close(); q := New(db)"
				sqlImport = "\"database/sql\""
			}
			source := strings.NewReplacer("$DSN", strconv.Quote(dsn), "$SCHEMA", strconv.Quote(schema), "$TABLE", strconv.Quote(table), "$COLLISION_SCHEMA", strconv.Quote(collisionSchema), "$COLLISION_TABLE", strconv.Quote(collisionTable), "$COLLISION_SQL", strconv.Quote(collisionSQL), "$SETUP", setup, "$SQL_IMPORT", sqlImport, "$METADATA", metadata.String()).Replace(sharedExpressionsRuntime)
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, false)
		})
	}
	t.Run("jooq", sharedExpressionsJooq)
}

const sharedExpressionsQueries = `-- name: Logic :many
SELECT id, flag AND enabled AS both, flag OR enabled AS either, flag XOR enabled AS differing,
       NOT flag AS inverted, flag IS NULL AS absent, flag IS DISTINCT FROM enabled AS distinct_flag,
       text || '!' AS appended, COALESCE(counter, 0l) AS counter_or_zero
FROM records ORDER BY id;

-- name: Statistics :one
SELECT COUNT(*) AS total, COUNT_IF(flag) AS flagged, COUNT_IF(NULL) AS null_count, CAST(COUNT(*) AS Bool)
FROM records;

-- name: EmptyStatistics :one
SELECT COUNT_IF(flag) AS flagged FROM records WHERE false;

-- name: Grouped :many
SELECT enabled, COUNT_IF(flag) AS flagged, COUNT_IF(flag IS NOT NULL) AS present
FROM records GROUP BY enabled ORDER BY enabled;

-- name: Casts :many
SELECT id, CAST(id AS Bool) AS active, CAST(enabled AS Uint64) AS numeric,
       CAST(id AS Uint32) AS narrow, CAST(text AS Json) AS document, COALESCE(CAST(id AS Uint32), 0)
FROM records ORDER BY id;

-- name: Names :one
SELECT 'last' AS z, 2 AS column2, 3, 4;

-- name: Mixed :many
SELECT t.*, COALESCE(counter, 0u) FROM records AS t ORDER BY column1;

-- name: WildcardCollision :one
SELECT t.*, 1, 2 AS column1 FROM records_collision AS t;

-- name: Builtins :one
SELECT CurrentUtcDate() AS today, CurrentUtcDatetime(1) AS seconds, CurrentUtcTimestamp(NULL) AS micros,
       Random(1) AS random_value, RandomNumber(1) AS random_number, RandomUuid(NULL) AS random_uuid,
       Version() AS version, NANVL(CAST('NaN' AS Double), 1.5) AS replacement,
       CAST(CurrentUtcTimestamp() AS String) AS timestamp_text,
       CAST(CurrentUtcTimestamp() AS Uint64) AS timestamp_number,
       CAST(18446744073709551615ul AS Timestamp) AS invalid_timestamp;
`

const sharedExpressionsRuntime = `package records

import (
	"context"
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"io"
	"reflect"
	$SQL_IMPORT
	"testing"
	"time"
)

func TestSharedExpressions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	driver, err := ydb.Open(ctx, $DSN, ydb.WithAnonymousCredentials())
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close(ctx)
	table := $TABLE
	if err := driver.Query().Exec(ctx, $SCHEMA); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := driver.Query().Exec(cleanup, "DROP TABLE "+table); err != nil {
			t.Error(err)
		}
	}()
	collisionTable := $COLLISION_TABLE
	if err := driver.Query().Exec(ctx, $COLLISION_SCHEMA); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := driver.Query().Exec(cleanup, "DROP TABLE "+collisionTable); err != nil {
			t.Error(err)
		}
	}()
	if err := driver.Query().Exec(ctx, "UPSERT INTO "+collisionTable+" (za,zb) VALUES ('after'u,18446744073709551615ul);"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Query().Exec(ctx, "UPSERT INTO "+table+" (id,enabled,flag,counter,text) VALUES (1ul,false,NULL,NULL,NULL),(2ul,true,true,7u,'{}'),(18446744073709551615ul,true,false,4294967295u,'bad');"); err != nil {
		t.Fatal(err)
	}
	$SETUP
	$METADATA
	rows, err := q.Logic(ctx)
	if err != nil || len(rows) != 3 {
		t.Fatalf("logic: %+v %v", rows, err)
	}
	first := rows[0]
	if first.Both == nil || *first.Both || first.Either != nil || first.Differing != nil || first.Inverted != nil || !first.Absent || !first.DistinctFlag || first.Appended != nil || first.CounterOrZero != 0 {
		t.Fatalf("nullable Boolean/string behavior: %+v", first)
	}
	second := rows[1]
	if second.Both == nil || !*second.Both || second.Either == nil || !*second.Either || second.Differing == nil || *second.Differing || second.Inverted == nil || *second.Inverted || second.Absent || second.DistinctFlag || second.Appended == nil || string(*second.Appended) != "{}!" || second.CounterOrZero != 7 {
		t.Fatalf("non-null behavior: %+v", second)
	}
	stats, err := q.Statistics(ctx)
	if err != nil || stats.Total != 3 || stats.Flagged != 1 || stats.NullCount != 0 || !stats.Column3 {
		t.Fatalf("global aggregate: %+v %v", stats, err)
	}
	empty, err := q.EmptyStatistics(ctx)
	if err != nil || empty.Flagged != 0 {
		t.Fatalf("empty aggregate: %+v %v", empty, err)
	}
	groups, err := q.Grouped(ctx)
	if err != nil || len(groups) != 2 || groups[0].Enabled || groups[0].Flagged != 0 || groups[0].Present != 0 || !groups[1].Enabled || groups[1].Flagged != 1 || groups[1].Present != 2 {
		t.Fatalf("grouped aggregate: %+v %v", groups, err)
	}
	casts, err := q.Casts(ctx)
	if err != nil || len(casts) != 3 {
		t.Fatalf("casts: %+v %v", casts, err)
	}
	if casts[0].Document != nil || casts[1].Document == nil || *casts[1].Document != "{}" || casts[2].Document != nil || casts[2].Narrow != nil || casts[2].Column5 != 0 || !casts[2].Active || casts[2].Numeric != 1 {
		t.Fatalf("cast validation/boundaries: %+v", casts)
	}
	names, err := q.Names(ctx)
	if err != nil || string(names.Z) != "last" || names.Column2 != 2 || names.Column3 != 3 || names.Column4 != 4 {
		t.Fatalf("collision names and positional order: %+v %v", names, err)
	}
	mixed, err := q.Mixed(ctx)
	if err != nil || len(mixed) != 3 || mixed[0].ID != 1 || mixed[0].Column1 != 0 || mixed[1].ID != 2 || mixed[1].Column1 != 7 {
		t.Fatalf("wildcard and original implicit ORDER BY name: %+v %v", mixed, err)
	}
	// The authored wildcard and the normalized explicit projection have different
	// wire orders; check each independently instead of deriving both from analysis.
	rawCollision := "SELECT t.*, 1, 2 AS column1 FROM "+collisionTable+" AS t;"
	checkMetadata(t, ctx, driver, rawCollision, []string{"column1", "column2", "za", "zb"}, []string{"Int32", "Int32", "Utf8", "Uint64"})
	checkMetadata(t, ctx, driver, $COLLISION_SQL, []string{"za", "zb", "column2", "column1"}, []string{"Utf8", "Uint64", "Int32", "Int32"})
	checkRawWildcardCollision(t, ctx, driver, rawCollision)
	collision, err := q.WildcardCollision(ctx)
	if err != nil || collision.Za != "after" || collision.Zb != ^uint64(0) || collision.Column1 != 2 || collision.Column2 != 1 {
		t.Fatalf("normalized wildcard collision positional decoding: %+v %v", collision, err)
	}
	before := time.Now().Add(-time.Minute)
	builtins, err := q.Builtins(ctx)
	if err != nil || builtins.Today.IsZero() || builtins.Seconds.Before(before) || builtins.Micros.Before(before) || builtins.RandomValue < 0 || builtins.RandomValue >= 1 || len(builtins.Version) == 0 || builtins.Replacement == nil || *builtins.Replacement != 1.5 || len(builtins.TimestampText) == 0 || builtins.TimestampNumber < uint64(before.UnixMicro()) || builtins.InvalidTimestamp != nil {
		t.Fatalf("builtins: %+v %v", builtins, err)
	}
	for _, statement := range []string{"SELECT COUNT_IF(true);", "SELECT COUNT(*) AS n;", "SELECT 1u AND true;", "SELECT Random();", "SELECT RandomNumber();", "SELECT RandomUuid();"} {
		if err := driver.Query().Exec(ctx, statement); err == nil {
			t.Errorf("invalid expression accepted: %s", statement)
		}
	}
}
func checkRawWildcardCollision(t *testing.T, ctx context.Context, driver *ydb.Driver, statement string) {
	t.Helper()
	result, err := driver.Query().Query(ctx, statement)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close(ctx)
	set, err := result.NextResultSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	row, err := set.NextRow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var column1, column2 int32
	var za string
	var zb uint64
	if err := row.Scan(&column1, &column2, &za, &zb); err != nil {
		t.Fatal(err)
	}
	if column1 != 2 || column2 != 1 || za != "after" || zb != ^uint64(0) {
		t.Fatalf("raw wildcard collision positional values: %d %d %q %d", column1, column2, za, zb)
	}
	t.Logf("raw wildcard collision wire order %v, values %d %d %q %d", set.Columns(), column1, column2, za, zb)
	if _, err := set.NextRow(ctx); err != io.EOF {
		t.Fatalf("extra collision row: %v", err)
	}
	if _, err := result.NextResultSet(ctx); err != io.EOF {
		t.Fatalf("extra collision result: %v", err)
	}
}
func checkMetadata(t *testing.T, ctx context.Context, driver *ydb.Driver, statement string, names, types []string) {
	t.Helper()
	result, err := driver.Query().Query(ctx, statement)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close(ctx)
	set, err := result.NextResultSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Columns(), names) {
		t.Fatalf("metadata names %v, want %v; SQL %s", set.Columns(), names, statement)
	}
	for i, typ := range set.ColumnTypes() {
		if typ.Yql() != types[i] {
			t.Fatalf("metadata %s: %s, want %s; SQL %s", names[i], typ.Yql(), types[i], statement)
		}
	}
	for {
		if _, err := set.NextRow(ctx); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := result.NextResultSet(ctx); err != io.EOF {
		t.Fatalf("result completion: %v", err)
	}
}
`
