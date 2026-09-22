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

	"github.com/ydb-platform/sqlc-ydb/internal/cli"
)

// Generation precedes ALTER TABLE so positional decoders exercise the saved SQL.
// The three analysis modes and their generated runtimes run sequentially.
func TestLiveYDBWildcardSchemaEvolution(t *testing.T) {
	if os.Getenv("YDB_CONNECTION_STRING") == "" {
		t.Skip("set YDB_CONNECTION_STRING for wildcard schema evolution")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_wildcard_evolution_%d", time.Now().UnixNano())
	runDatabasePython(t, dir, wildcardFixturePython, "create", table)
	t.Cleanup(func() { runDatabasePython(t, dir, wildcardFixturePython, "drop", table) })
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema.sql", "CREATE TABLE "+table+" (ztext Utf8, id Uint64 NOT NULL, amount Int32, PRIMARY KEY(id));")
	queries := strings.ReplaceAll(`-- name: ReadWildcard :one
DECLARE $id AS Uint64;
-- Preserve this * comment.
SELECT * FROM records WHERE id = $id;

-- name: ReadQualifiedWildcard :one
DECLARE $id AS Uint64;
SELECT r.* FROM records AS r WHERE r.id = $id;

-- name: InsertReturningWildcard :one
DECLARE $id AS Uint64;
DECLARE $ztext AS Optional<Utf8>;
DECLARE $amount AS Optional<Int32>;
INSERT INTO records (id, ztext, amount) VALUES ($id, $ztext, $amount) RETURNING *;

-- name: UpdateReturningWildcard :one
DECLARE $id AS Uint64;
DECLARE $ztext AS Optional<Utf8>;
DECLARE $amount AS Optional<Int32>;
UPDATE records SET ztext = $ztext, amount = $amount WHERE id = $id RETURNING *;

-- name: DeleteReturningWildcard :one
DECLARE $id AS Uint64;
DELETE FROM records WHERE id = $id RETURNING *;
`, "records", table)
	write("queries.sql", queries)
	write("go.mod", "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n")
	profiles := []struct {
		name     string
		settings string
	}{
		{"offline", "  schema: schema.sql\n"},
		{"discovery", "  database:\n    uri: ${YDB_CONNECTION_STRING}\n"},
		{"checked_schema", "  schema: schema.sql\n  database:\n    uri: ${YDB_CONNECTION_STRING}\n"},
	}
	for _, profile := range profiles {
		write(profile.name+".yaml", "version: '2'\nsql:\n- engine: ydb\n  queries: queries.sql\n"+profile.settings+"  gen:\n    go:\n      package: db\n      sql_package: database/sql\n      out: "+profile.name+"\n")
		var stdout, stderr bytes.Buffer
		if code := cli.Run([]string{"generate", "--no-remote", "-f", filepath.Join(dir, profile.name+".yaml")}, &stdout, &stderr); code != 0 {
			t.Fatalf("generate %s: %s", profile.name, stderr.String())
		}
		generated := string(mustRead(t, filepath.Join(dir, profile.name, "queries.sql.go")))
		for _, wildcard := range []string{"SELECT *", "SELECT r.*", "RETURNING *"} {
			if strings.Contains(generated, wildcard) {
				t.Errorf("%s still emits %q; saved SQL will change result shape after ALTER TABLE", profile.name, wildcard)
			}
		}
		if !strings.Contains(generated, "-- Preserve this * comment.") {
			t.Errorf("%s changed an unrelated SQL comment", profile.name)
		}
	}
	if got := string(mustRead(t, filepath.Join(dir, "queries.sql"))); got != queries {
		t.Fatal("generation modified the query source")
	}

	runDatabasePython(t, dir, wildcardFixturePython, "alter", table)
	for index, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			compileTypedDMLPackage(t, dir, "./"+profile.name, wildcardGeneratedGo(uint64(100+index)), false)
		})
	}
}

const wildcardFixturePython = databasePythonConnection + `import sys
mode, table = sys.argv[1:]
with ydb.Driver(config) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        if mode == "create":
            pool.execute_with_retries("CREATE TABLE " + table + " (ztext Utf8, id Uint64 NOT NULL, amount Int32, PRIMARY KEY(id));")
            pool.execute_with_retries("UPSERT INTO " + table + " (id, ztext, amount) VALUES (42ul, 'original'u, -7);")
        elif mode == "alter":
            pool.execute_with_retries("ALTER TABLE " + table + " ADD COLUMN aextra Utf8;")
            pool.execute_with_retries("UPDATE " + table + " SET aextra = 'new column'u WHERE id = 42ul;")
            result = pool.execute_with_retries("SELECT * FROM " + table + " WHERE id = 42ul;")[0]
            assert [column.name for column in result.columns] == ["aextra", "amount", "id", "ztext"], result.columns
            assert result.rows[0]["aextra"] == "new column", result.rows
        else:
            pool.execute_with_retries("DROP TABLE IF EXISTS " + table + ";")
`

func wildcardGeneratedGo(insertedID uint64) string {
	return `package db
import (
    "context"
    "database/sql"
    "os"
    "reflect"
    "testing"
    "time"
    ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)
func TestGeneratedWildcardsAfterAddedColumn(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
    defer cancel()
    driver, err := ydb.Open(ctx, os.Getenv("YDB_CONNECTION_STRING"), ydb.WithAnonymousCredentials())
    if err != nil { t.Fatal(err) }
    defer driver.Close(ctx)
    db := sql.OpenDB(ydb.MustConnector(driver))
    defer db.Close()
    q := New(db)
    check := func(t *testing.T, row any, id uint64, amount int32, text string) {
        t.Helper()
        value := reflect.ValueOf(row)
        if value.NumField() != 3 { t.Fatalf("added column leaked into saved API: %+v", row) }
        actualID, ok := value.FieldByName("ID").Interface().(uint64)
        if !ok || actualID != id { t.Fatalf("wrong id in %+v", row) }
        actualAmount, ok := value.FieldByName("Amount").Interface().(*int32)
        if !ok || actualAmount == nil || *actualAmount != amount { t.Fatalf("wrong amount in %+v", row) }
        actualText, ok := value.FieldByName("Ztext").Interface().(*string)
        if !ok || actualText == nil || *actualText != text { t.Fatalf("wrong text in %+v", row) }
    }
    t.Run("select_star", func(t *testing.T) {
        row, err := q.ReadWildcard(ctx, 42)
        if err != nil { t.Fatal(err) }
        check(t, row, 42, -7, "original")
    })
    t.Run("qualified_star", func(t *testing.T) {
        row, err := q.ReadQualifiedWildcard(ctx, 42)
        if err != nil { t.Fatal(err) }
        check(t, row, 42, -7, "original")
    })
    t.Run("returning_star", func(t *testing.T) {
        const id uint64 = ` + strconv.FormatUint(insertedID, 10) + `
        amount, text := int32(9), "inserted"
        inserted, err := q.InsertReturningWildcard(ctx, InsertReturningWildcardParams{ID: id, Ztext: &text, Amount: &amount})
        if err != nil { t.Fatal(err) }
        check(t, inserted, id, amount, text)
        amount, text = 17, "updated"
        updated, err := q.UpdateReturningWildcard(ctx, UpdateReturningWildcardParams{ID: id, Ztext: &text, Amount: &amount})
        if err != nil { t.Fatal(err) }
        check(t, updated, id, amount, text)
        deleted, err := q.DeleteReturningWildcard(ctx, id)
        if err != nil { t.Fatal(err) }
        check(t, deleted, id, amount, text)
        if _, err := q.ReadWildcard(ctx, id); err == nil { t.Fatal("deleted row still exists") }
    })
}
`
}
