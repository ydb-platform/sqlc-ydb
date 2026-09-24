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

func TestInSubqueriesGeneratedGoCompiles(t *testing.T) {
	runInSubqueries(t, "grpc://localhost:2136/local", true)
}

func TestLiveYDBInSubqueries(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for IN subquery validation")
	}
	runInSubqueries(t, dsn, false)
	t.Run("jooq", inSubqueriesJooq)
}

func runInSubqueries(t *testing.T, dsn string, compileOnly bool) {
	t.Helper()
	dir := t.TempDir()
	table := fmt.Sprintf("sqlc_in_subqueries_%d", time.Now().UnixNano())
	replace := strings.NewReplacer("records", table, "memberships", table+"_keys", "nullable_keys", table+"_nullable")
	schema := replace.Replace(inSubqueriesSchema)
	queries := replace.Replace(inSubqueriesQueries)
	configuration := "version: '2'\nsql:\n"
	for _, runtime := range []string{"ydb", "database/sql"} {
		configuration += "- engine: ydb\n  schema: schema.sql\n  queries: queries.sql\n  gen:\n    go:\n      package: records\n      out: " + strings.ReplaceAll(runtime, "/", "_") + "\n      sql_package: " + runtime + "\n"
	}
	for name, contents := range map[string]string{"schema.sql": schema, "queries.sql": queries, "sqlc.yaml": configuration, "go.mod": "module generated\n\ngo 1.26.0\n\nrequire github.com/ydb-platform/ydb-go-sdk/v3 v3.151.1\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600))
	}
	var stdout, stderr bytes.Buffer
	require.Zero(t, cli.Run([]string{"generate", "-f", filepath.Join(dir, "sqlc.yaml")}, &stdout, &stderr), "generate IN subqueries: %s", stderr.String())
	for _, runtime := range []string{"ydb", "database/sql"} {
		t.Run(runtime, func(t *testing.T) {
			setup, sqlImport := "q := New(driver.Query())", ""
			if runtime == "database/sql" {
				setup = "db := sql.OpenDB(ydb.MustConnector(driver)); defer db.Close(); q := New(db)"
				sqlImport = "\"database/sql\""
			}
			source := strings.NewReplacer("$DSN", strconv.Quote(dsn), "$SCHEMA", strconv.Quote(schema), "$TABLE", strconv.Quote(table), "$SETUP", setup, "$SQL_IMPORT", sqlImport).Replace(inSubqueriesRuntime)
			compileTypedDMLPackage(t, dir, "./"+strings.ReplaceAll(runtime, "/", "_"), source, compileOnly)
		})
	}
}

const inSubqueriesSchema = `CREATE TABLE records (id Uint64 NOT NULL, code Utf8 NOT NULL, value Uint64, active Bool NOT NULL, PRIMARY KEY(id,code));
CREATE TABLE memberships (id Uint64 NOT NULL, code Utf8 NOT NULL, PRIMARY KEY(id,code));
CREATE TABLE nullable_keys (id Uint64, code Utf8, PRIMARY KEY(id,code));`

const inSubqueriesQueries = `-- name: ListRecords :many
SELECT id,code,value,active FROM records ORDER BY id,code;

-- name: ScalarKeys :many
DECLARE $keys AS List<Struct<id:Uint64>>;
DECLARE $minimum AS Uint64;
SELECT r.id,r.code FROM records AS r
WHERE r.id IN (SELECT k.id FROM AS_TABLE($keys) AS k WHERE k.id >= $minimum)
ORDER BY r.id,r.code;

-- name: NullableKeys :many
DECLARE $keys AS List<Struct<value:Uint64?>>;
SELECT id,code FROM records
WHERE value IN (SELECT k.value FROM AS_TABLE($keys) AS k)
ORDER BY id,code;

-- name: NullableNotIn :many
DECLARE $keys AS List<Struct<value:Uint64?>>;
SELECT id,code FROM records
WHERE value NOT IN (SELECT k.value FROM AS_TABLE($keys) AS k)
ORDER BY id,code;

-- name: TupleKeys :many
DECLARE $keys AS List<Struct<id:Uint64,code:Utf8>>;
SELECT id,code FROM records
WHERE (id,code) IN (SELECT (k.id,k.code) FROM AS_TABLE($keys) AS k)
ORDER BY id,code;

-- name: NullableTupleKeys :many
DECLARE $keys AS List<Struct<id:Uint64,code:Utf8>>;
SELECT id,code FROM nullable_keys
WHERE (id,code) IN (SELECT (k.id,k.code) FROM AS_TABLE($keys) AS k)
ORDER BY id,code;

-- name: ActivateKeys :exec
DECLARE $keys AS List<Struct<id:Uint64,code:Utf8>>;
UPDATE records SET active = true
WHERE (id,code) IN (SELECT (k.id,k.code) FROM AS_TABLE($keys) AS k);

-- name: DeleteKeys :exec
DECLARE $keys AS List<Struct<id:Uint64,code:Utf8>>;
DELETE FROM records
WHERE (id,code) IN (SELECT (k.id,k.code) FROM AS_TABLE($keys) AS k);

-- name: ShadowedAliases :many
DECLARE $code AS Utf8;
SELECT r.id,r.code FROM records AS r
WHERE r.id IN (SELECT r.id FROM memberships AS r WHERE r.code = $code)
ORDER BY r.id,r.code;

-- name: TableKeys :many
SELECT id,code FROM records
WHERE (id,code) IN (SELECT (k.id,k.code) FROM memberships AS k)
ORDER BY id,code;
`

const inSubqueriesRuntime = `package records

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	$SQL_IMPORT
	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
)

func TestInSubqueries(t *testing.T) {
	ctx,cancel := context.WithTimeout(context.Background(),90*time.Second)
	defer cancel()
	driver,err := ydb.Open(ctx,$DSN,ydb.WithAnonymousCredentials())
	if err != nil {t.Fatal(err)}
	defer driver.Close(ctx)
	table := $TABLE
	for _,ddl := range strings.Split($SCHEMA,";") {
		if strings.TrimSpace(ddl)=="" {continue}
		if err := driver.Query().Exec(ctx,ddl); err != nil {t.Fatal(err)}
		created := strings.Fields(ddl)[2]
		defer func() {
			cleanup,done := context.WithTimeout(context.Background(),10*time.Second)
			defer done()
			if err := driver.Query().Exec(cleanup,"DROP TABLE "+created);err != nil {t.Error(err)}
		}()
	}
	if err := driver.Query().Exec(ctx,"UPSERT INTO "+table+" (id,code,value,active) VALUES (1ul,'a'u,10ul,false),(2ul,'b'u,20ul,false),(3ul,'c'u,NULL,false),(18446744073709551615ul,'max'u,18446744073709551615ul,false);");err != nil {t.Fatal(err)}
	if err := driver.Query().Exec(ctx,"UPSERT INTO "+table+"_keys (id,code) VALUES (1ul,'a'u),(2ul,'wrong'u),(18446744073709551615ul,'max'u);");err != nil {t.Fatal(err)}
	if err := driver.Query().Exec(ctx,"UPSERT INTO "+table+"_nullable (id,code) VALUES (1ul,'a'u),(2ul,'b'u),(3ul,NULL),(NULL,'a'u),(18446744073709551615ul,'max'u);");err != nil {t.Fatal(err)}
	$SETUP
	all := []string{"1:a","2:b","3:c","18446744073709551615:max"}
	pair := []string{"1:a","18446744073709551615:max"}
	for _,keys := range [][]ScalarKeysKeysItem{nil,{}} {
		rows,err := q.ScalarKeys(ctx,ScalarKeysParams{Keys:keys,Minimum:0})
		checkKeys(t,rows,err,nil)
	}
	rows,err := q.ScalarKeys(ctx,ScalarKeysParams{Keys:[]ScalarKeysKeysItem{{ID:1},{ID:3},{ID:^uint64(0)}},Minimum:3})
	checkKeys(t,rows,err,[]string{"3:c","18446744073709551615:max"})
	rows,err = q.ScalarKeys(ctx,ScalarKeysParams{Keys:[]ScalarKeysKeysItem{{ID:1}},Minimum:2})
	checkKeys(t,rows,err,nil)
	value := uint64(10)
	nulls,err := q.NullableKeys(ctx,[]NullableKeysKeysItem{{Value:&value},{Value:nil}})
	checkKeys(t,nulls,err,[]string{"1:a"})
	notIn,err := q.NullableNotIn(ctx,[]NullableNotInKeysItem{{Value:&value},{Value:nil}})
	checkKeys(t,notIn,err,nil)
	notIn,err = q.NullableNotIn(ctx,nil)
	checkKeys(t,notIn,err,all)
	for _,keys := range [][]TupleKeysKeysItem{nil,{},{{ID:1,Code:"a"},{ID:1,Code:"a"},{ID:2,Code:"wrong"},{ID:^uint64(0),Code:"max"}}} {
		tuples,err := q.TupleKeys(ctx,keys)
		var want []string
		if len(keys)>0 {want=pair}
		checkKeys(t,tuples,err,want)
	}
	optionalTuples,err := q.NullableTupleKeys(ctx,[]NullableTupleKeysKeysItem{{ID:1,Code:"a"},{ID:2,Code:"wrong"},{ID:3,Code:"a"},{ID:^uint64(0),Code:"max"}})
	if err != nil {t.Fatal(err)}
	if len(optionalTuples)!=2 || optionalTuples[0].ID==nil || *optionalTuples[0].ID!=1 || optionalTuples[0].Code==nil || *optionalTuples[0].Code!="a" || optionalTuples[1].ID==nil || *optionalTuples[1].ID!=^uint64(0) || optionalTuples[1].Code==nil || *optionalTuples[1].Code!="max" {t.Fatalf("nullable tuple comparison/decoding: %+v",optionalTuples)}
	optionalTuples,err = q.NullableTupleKeys(ctx,nil)
	if err != nil || len(optionalTuples)!=0 {t.Fatalf("empty nullable tuple keys: %+v, %v",optionalTuples,err)}
	shadow,err := q.ShadowedAliases(ctx,"a")
	checkKeys(t,shadow,err,[]string{"1:a"})
	shadow,err = q.ShadowedAliases(ctx,"absent")
	checkKeys(t,shadow,err,nil)
	physical,err := q.TableKeys(ctx)
	checkKeys(t,physical,err,pair)
	for _,keys := range [][]ActivateKeysKeysItem{nil,{},{{ID:1,Code:"a"},{ID:2,Code:"wrong"},{ID:^uint64(0),Code:"max"}}} {
		if err := q.ActivateKeys(ctx,keys);err != nil {t.Fatal(err)}
	}
	records,err := q.ListRecords(ctx)
	checkKeys(t,records,err,all)
	if !records[0].Active || records[1].Active || records[2].Active || !records[3].Active || records[2].Value != nil || records[3].Value == nil || *records[3].Value != ^uint64(0) {
		t.Fatalf("tuple update or nullable/boundary decoding: %+v",records)
	}
	checkInMetadata(t,ctx,driver,"SELECT r.id,r.code FROM "+table+" r WHERE (r.id,r.code) IN (SELECT(k.id,k.code) FROM "+table+"_keys k) ORDER BY r.id,r.code;",[]string{"id","code"},[]string{"Uint64","Utf8"})
	checkInMetadata(t,ctx,driver,"SELECT value FROM "+table+" WHERE value IN (SELECT value FROM "+table+" WHERE false);",[]string{"value"},[]string{"Optional<Uint64>"})
	for _,predicate := range []string{
		"(r.id,r.code) IN (SELECT k.id,k.code FROM "+table+"_keys k)",
		"(r.id,r.code) IN (SELECT(k.id,k.code,k.id) FROM "+table+"_keys k)",
		"(r.id,r.code) IN (SELECT(k.code,k.id) FROM "+table+"_keys k)",
		"r.id IN (SELECT k.code FROM "+table+"_keys k)",
		"r.id IN (SELECT k.id FROM "+table+"_keys k WHERE k.id=r.id)",
		"r.id IN (SELECT k.id FROM "+table+"_keys k WHERE value=10ul)",
	} {
		if err := driver.Query().Exec(ctx,"SELECT r.id FROM "+table+" r WHERE "+predicate+";");err == nil {t.Errorf("server accepted invalid/correlated IN: %s",predicate)}
	}
	for _,keys := range [][]DeleteKeysKeysItem{nil,{},{{ID:1,Code:"wrong"},{ID:2,Code:"b"}}} {
		if err := q.DeleteKeys(ctx,keys);err != nil {t.Fatal(err)}
	}
	records,err = q.ListRecords(ctx)
	checkKeys(t,records,err,[]string{"1:a","3:c","18446744073709551615:max"})
}

func checkKeys(t *testing.T,rows any,err error,want []string) {
	t.Helper()
	if err != nil {t.Fatal(err)}
	var got []string
	values:=reflect.ValueOf(rows)
	for i:=0;i<values.Len();i++ {
		row:=values.Index(i)
		got=append(got,fmt.Sprintf("%d:%s",row.FieldByName("ID").Uint(),row.FieldByName("Code").String()))
	}
	if !reflect.DeepEqual(got,want) {t.Fatalf("rows=%v, want %v",got,want)}
}

func checkInMetadata(t *testing.T,ctx context.Context,driver *ydb.Driver,statement string,names,types []string) {
	t.Helper()
	result,err:=driver.Query().Query(ctx,statement)
	if err!=nil {t.Fatal(err)}
	defer result.Close(ctx)
	set,err:=result.NextResultSet(ctx)
	if err!=nil {t.Fatal(err)}
	if !reflect.DeepEqual(set.Columns(),names)||len(set.ColumnTypes())!=len(types) {t.Fatalf("columns %v, want %v",set.Columns(),names)}
	for i,typ:=range set.ColumnTypes() {if typ.Yql()!=types[i] {t.Fatalf("type %s, want %s",typ.Yql(),types[i])}}
	for {if _,err:=set.NextRow(ctx);err==io.EOF {break}else if err!=nil {t.Fatal(err)}}
	if _,err:=result.NextResultSet(ctx);err!=io.EOF {t.Fatalf("result completion: %v",err)}
}
`
