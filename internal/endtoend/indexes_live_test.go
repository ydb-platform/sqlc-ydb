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

// Compile and execute both generated Go adapters against isolated indexed tables.
func TestLiveYDBIndexes(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for live index validation")
	}
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_indexes_%d", time.Now().UnixNano())
	schema := strings.ReplaceAll(`CREATE TABLE records (id Uint64 NOT NULL, tag Utf8 NOT NULL, detail Utf8,
 INDEX by_tag GLOBAL SYNC ON(tag), INDEX covered GLOBAL SYNC ON(tag) COVER(detail), INDEX async_tag GLOBAL ASYNC ON(tag), PRIMARY KEY(id));
CREATE TABLE copies (id Uint64 NOT NULL, tag Utf8 NOT NULL, detail Utf8, PRIMARY KEY(id));`, "records", table)
	schema = strings.ReplaceAll(schema, "copies", table+"_copies")
	queries := strings.ReplaceAll(`-- name: ReadIndex :many
SELECT r.* FROM records VIEW by_tag AS r WHERE r.tag = $tag ORDER BY r.id;
-- name: ReadCover :many
SELECT * FROM records VIEW covered WHERE tag = $tag ORDER BY id;
-- name: ReadAsync :many
SELECT id FROM records VIEW async_tag WHERE tag = $tag ORDER BY id;
-- name: CopyIndex :exec
INSERT INTO copies SELECT r.* FROM records VIEW by_tag AS r WHERE r.tag = $tag;
-- name: RefreshCopy :exec
UPSERT INTO copies SELECT id, tag, detail FROM records VIEW covered WHERE tag = $tag;
-- name: ReadCopies :many
SELECT * FROM copies ORDER BY id;`, "records", table)
	queries = strings.ReplaceAll(queries, "copies", table+"_copies")
	config := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		out := strings.ReplaceAll(runtime, "/", "_")
		config += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + out + "\n      sql_package: " + runtime + "\n"
	}
	for name, content := range map[string]string{"schema.sql": schema, "queries.sql": queries, "sqlc.yaml": config, "go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr); code != 0 {
		t.Fatalf("generate: %s", stderr.String())
	}
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup := "q := New(driver.Query())"
			imports := ""
			async := `
 if _,err:=q.ReadAsync(ctx,"same");err==nil || !strings.Contains(err.Error(),"StaleRO") {t.Fatalf("async default transaction must be rejected: %v",err)}
 deadline:=time.Now().Add(15*time.Second)
 for {
  rows,err:=q.ReadAsync(ctx,"same",query.WithTxControl(query.StaleReadOnlyTxControl()))
  if err!=nil {t.Fatal(err)}
  if len(rows)==2 && rows[0].ID==1 && rows[1].ID==2 {break}
  if time.Now().After(deadline) {t.Fatalf("async index did not converge: %v",rows)}
  time.Sleep(100*time.Millisecond)
 }
`
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver)); defer db.Close(); q := New(db)"
				imports = "\"database/sql\""
				async = ""
			} else {
				imports = "\"github.com/ydb-platform/ydb-go-sdk/v3/query\"\n\"strings\""
			}
			source := `package records
import("context";"testing";"time";` + imports + `;ydb "github.com/ydb-platform/ydb-go-sdk/v3")
func TestIndexes(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),60*time.Second);defer cancel()
 driver,err:=ydb.Open(ctx,` + strconv.Quote(dsn) + `,ydb.WithAnonymousCredentials());if err!=nil {t.Fatal(err)};defer driver.Close(ctx)
 if err:=driver.Query().Exec(ctx,` + strconv.Quote(schema) + `);err!=nil {t.Fatal(err)}
 defer func(){cleanup,done:=context.WithTimeout(context.Background(),10*time.Second);defer done();for _,table:=range []string{` + strconv.Quote(table+"_copies") + `,` + strconv.Quote(table) + `}{if err:=driver.Query().Exec(cleanup,"DROP TABLE "+table);err!=nil {t.Error(err)}}}()
 if err:=driver.Query().Exec(ctx,` + strconv.Quote("UPSERT INTO "+table+" (id,tag,detail) VALUES (1ul,'same','initial'),(2ul,'same',NULL),(3ul,'other','excluded');") + `);err!=nil {t.Fatal(err)}
 ` + setup + `
 rows,err:=q.ReadIndex(ctx,"same");if err!=nil || len(rows)!=2 || rows[0].ID!=1 || rows[0].Detail==nil || *rows[0].Detail!="initial" || rows[1].Detail!=nil {t.Fatalf("noncovering: %v %v",rows,err)}
 covered,err:=q.ReadCover(ctx,"same");if err!=nil || len(covered)!=2 || covered[0].Detail==nil || *covered[0].Detail!="initial" || covered[1].Detail!=nil {t.Fatalf("covering: %v %v",covered,err)}
 rows,err=q.ReadIndex(ctx,"missing");if err!=nil || len(rows)!=0 {t.Fatalf("empty index: %v %v",rows,err)}
 if err:=q.CopyIndex(ctx,"same");err!=nil {t.Fatal(err)}
 copied,err:=q.ReadCopies(ctx);if err!=nil || len(copied)!=2 || copied[0].Detail==nil || *copied[0].Detail!="initial" || copied[1].Detail!=nil {t.Fatalf("INSERT SELECT: %v %v",copied,err)}
 if err:=driver.Query().Exec(ctx,` + strconv.Quote("UPDATE "+table+" SET detail='updated' WHERE id=1ul;") + `);err!=nil {t.Fatal(err)}
 if err:=q.RefreshCopy(ctx,"same");err!=nil {t.Fatal(err)}
 copied,err=q.ReadCopies(ctx);if err!=nil || len(copied)!=2 || copied[0].Detail==nil || *copied[0].Detail!="updated" {t.Fatalf("UPSERT SELECT: %v %v",copied,err)}
 ` + async + `
}
`
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, false)
		})
	}
}
