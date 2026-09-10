package endtoend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ydb-platform/sqlc-ydb/internal/analyzer"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Verify server column names, order and full types independently of renderers.
// Run only against an explicitly supplied disposable test database.
func TestLiveYDBSemanticTypes(t *testing.T) {
	dsn := os.Getenv("YDB_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("set YDB_CONNECTION_STRING for live semantic validation")
	}
	table := fmt.Sprintf("sqlc_semantic_%d", time.Now().UnixNano())
	schema := "CREATE TABLE " + table + " (id Uint64 NOT NULL, flag Bool NOT NULL, n Int32 NOT NULL, f Float NOT NULL, maybe Int32, label Utf8, stamp Timestamp NOT NULL, amount Decimal(22,9), PRIMARY KEY(id));"
	queries := []struct{ Name, SQL string }{
		{"casts", "SELECT CAST(n AS Int64) AS wide, CAST(n AS Uint8) AS narrow, CAST(f AS Int32) AS integer_value FROM $TABLE;"},
		{"case", "SELECT CASE WHEN flag THEN n ELSE maybe END AS choice, CASE n WHEN 1 THEN 1u ELSE 2 END AS mixed FROM $TABLE;"},
		{"coalesce_explicit_numeric", "SELECT COALESCE(CAST(n AS Int64), 1l) AS required, COALESCE(CAST(maybe AS Int64), 1l) AS fallback FROM $TABLE;"},
		{"substring_positions", "SELECT SUBSTRING(\"abc\", CAST(n AS Uint8)) AS small, SUBSTRING(\"abc\", CAST(n AS Uint16)) AS medium, SUBSTRING(\"abc\", CAST(id AS Uint32)) AS wide FROM $TABLE;"},
		{"find_positions", "SELECT FIND(\"abc\", \"a\", CAST(n AS Uint16)) AS first, RFIND(\"abc\", \"a\", CAST(id AS Uint32)) AS last FROM $TABLE;"},
		{"union_join_missing", "SELECT a.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id UNION ALL SELECT b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id;"},
		{"union_join_names", "SELECT a.id, b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id UNION ALL SELECT a.id, b.id FROM $TABLE AS a JOIN $TABLE AS b ON a.id=b.id;"},
		{"coalesce", "SELECT COALESCE(maybe, n) AS value, LENGTH(COALESCE(label, \"fallback\"u)) AS size FROM $TABLE;"},
		{"aggregate", "SELECT COUNT(*) AS count, SUM(n) AS total, AVG(f) AS mean, MIN(f) AS minimum, MAX(maybe) AS maximum FROM $TABLE;"},
		{"group", "SELECT id, SUM(n) AS total, AVG(f) AS mean, MIN(f) AS minimum, MAX(maybe) AS maximum FROM $TABLE GROUP BY id HAVING COUNT(*) > 0ul;"},
		{"group_nested", "SELECT id, COALESCE(SUM(n), 0l) AS total FROM $TABLE GROUP BY id;"},
		{"case_comparison", "SELECT CASE WHEN n > 1 THEN maybe ELSE n END AS value FROM $TABLE;"},
		{"if_comparison", "SELECT IF(n = 1, maybe, n) AS value FROM $TABLE;"},
		{"union_new_order", "SELECT 1 AS z UNION ALL SELECT 2 AS y, 3 AS a;"},
		{"union_three_arms", "SELECT 1 AS z UNION ALL SELECT 2 AS y, 3 AS d UNION ALL SELECT 4 AS b, 5 AS a;"},
		{"union_order", "SELECT 1 AS z, 2 AS a UNION ALL SELECT 3 AS a, 4 AS z;"},
		{"union_prefix", "SELECT 1 AS id, 2 AS z, 3 AS a UNION ALL SELECT 4 AS id, 5 AS a, 6 AS z;"},
		{"union_missing", "SELECT 1 AS z UNION ALL SELECT 2 AS a;"},
		{"union_names", "SELECT 1 AS z, 2 AS b UNION ALL SELECT 3 AS a, 4 AS b;"},
		{"union_types", "SELECT n AS value FROM $TABLE UNION SELECT maybe AS value FROM $TABLE;"},
		{"mixed_integers", "SELECT CASE WHEN TRUE THEN 1l ELSE 1ul END AS wide, CASE WHEN TRUE THEN 1 ELSE 1u END AS small;"},
		{"libraries", "SELECT String::AsciiToLower(\"ABC\") AS lower, Unicode::GetLength(\"текст\"u) AS size;"},
	}
	queries = append(queries, builtinLiveQueries()...)
	queries = append(queries, struct{ Name, SQL string }{"decimal_aggregates", "SELECT SUM(amount) AS total, AVG(amount) AS mean FROM $TABLE;"})
	type column struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type liveCase struct {
		Name    string   `json:"name"`
		SQL     string   `json:"sql"`
		Columns []column `json:"columns"`
	}
	cases := make([]liveCase, 0, len(queries))
	for _, q := range queries {
		sql := strings.ReplaceAll(q.SQL, "$TABLE", table)
		a, err := analyzer.Analyze([]model.Source{{Name: "schema.sql", Text: schema}}, []model.Source{{Name: q.Name + ".sql", Text: "-- name: Check :many\n" + sql}})
		if err != nil {
			t.Errorf("%s: %v", q.Name, err)
			continue
		}
		c := liveCase{Name: q.Name, SQL: sql}
		for _, col := range a.Queries[0].ResultSets[0].Columns {
			c.Columns = append(c.Columns, column{col.ResultName(), semanticTypeName(col.Type)})
		}
		cases = append(cases, c)
	}
	if t.Failed() {
		t.FailNow()
	}
	payload, err := json.Marshal(struct {
		Schema, Table string
		Cases         []liveCase
	}{schema, table, cases})
	if err != nil {
		t.Fatal(err)
	}
	const script = `import json, os, sys, urllib.parse
import ydb
payload = json.load(sys.stdin)
u = urllib.parse.urlsplit(os.environ["YDB_CONNECTION_STRING"])
def typename(t):
    if t.HasField("optional_type"): return "Optional<" + typename(t.optional_type.item) + ">"
    if t.HasField("list_type"): return "List<" + typename(t.list_type.item) + ">"
    if t.HasField("decimal_type"): return "Decimal(%s,%s)" % (t.decimal_type.precision, t.decimal_type.scale)
    if t.HasField("type_id"): return t.DESCRIPTOR.fields_by_name["type_id"].enum_type.values_by_number[t.type_id].name.lower()
    raise AssertionError("unhandled server type: %s" % t)
errors = []
with ydb.Driver(ydb.DriverConfig(u.scheme+"://"+u.netloc,u.path,credentials=ydb.AnonymousCredentials(),disable_discovery=True)) as driver:
    driver.wait(20)
    with ydb.QuerySessionPool(driver) as pool:
        pool.execute_with_retries(payload["Schema"])
        try:
            for case in payload["Cases"]:
                try:
                    result = pool.execute_with_retries(case["sql"])[0]
                    actual = [{"name": c.name, "type": typename(c.type)} for c in result.columns]
                    assert actual == case["columns"], "expected %s; got %s" % (case["columns"], actual)
                    print(case["name"]+": matches YDB", flush=True)
                except Exception as e: errors.append(case["name"]+": "+str(e))
        finally: pool.execute_with_retries("DROP TABLE "+payload["Table"]+";")
assert not errors, "\n".join(errors)
`
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", script)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("live semantic validation: %v\n%s", err, out)
	}
	t.Log(string(out))
}

func semanticTypeName(typ model.Type) string {
	switch typ.Kind {
	case "Optional", "List":
		return typ.Kind + "<" + semanticTypeName(*typ.Elem) + ">"
	case "Decimal":
		return fmt.Sprintf("Decimal(%d,%d)", typ.Precision, typ.Scale)
	default:
		return strings.ToLower(typ.Kind)
	}
}
