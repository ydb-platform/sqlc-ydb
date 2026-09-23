package analyzer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const inSubquerySchema = `CREATE TABLE records (
    tenant Uint64 NOT NULL,
    code Utf8 NOT NULL,
    enabled Bool,
    PRIMARY KEY (tenant, code)
);
CREATE TABLE allowed (tenant Uint64 NOT NULL, code Utf8 NOT NULL, PRIMARY KEY (tenant, code));
CREATE TABLE numeric_codes (code Uint64 NOT NULL, PRIMARY KEY(code));`

func TestINSubqueryResolvesIndependentScopes(t *testing.T) {
	for _, tc := range []struct {
		name, command, sql string
		wantParameter      string
	}{
		{"declared list source", ":many", `DECLARE $keys AS List<Struct<code:Utf8>>;
SELECT code FROM records WHERE code IN (SELECT code FROM AS_TABLE($keys));`, "List<Struct<code:Utf8>>"},
		{"catalog inner filter", ":many", `SELECT r.code FROM records AS r WHERE r.code IN (SELECT a.code FROM allowed AS a WHERE a.tenant = $tenant);`, "Uint64"},
		{"tuple update", ":exec", `DECLARE $keys AS List<Struct<tenant:Uint64,code:Utf8>>;
UPDATE records SET enabled = true WHERE (tenant, code) IN (SELECT (k.tenant, k.code) FROM AS_TABLE($keys) AS k);`, "List<Struct<code:Utf8,tenant:Uint64>>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := "-- name: Read " + tc.command + "\n" + tc.sql
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: inSubquerySchema}}, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Queries) != 1 {
				t.Fatalf("queries = %#v", result.Queries)
			}
			query := result.Queries[0]
			if query.SQL != sql {
				t.Fatalf("SQL changed: %q", query.SQL)
			}
			want, err := parseType(tc.wantParameter)
			if err != nil {
				t.Fatal(err)
			}
			if len(query.Parameters) != 1 || !query.Parameters[0].Type.Equal(want) {
				t.Fatalf("parameters = %+v, want %s", query.Parameters, tc.wantParameter)
			}
			if tc.command == ":many" {
				if len(query.ResultSets) != 1 || len(query.ResultSets[0].Columns) != 1 || query.ResultSets[0].Columns[0].Type.String() != "Utf8" {
					t.Fatalf("outer result = %+v", query.ResultSets)
				}
			} else if len(query.ResultSets) != 0 {
				t.Fatalf("inner SELECT leaked into result: %+v", query.ResultSets)
			}
		})
	}
}

func TestINSubqueryPredicateForms(t *testing.T) {
	for _, tc := range []struct {
		name, command, sql string
		columns            int
	}{
		{"shadowed alias", ":many", `SELECT r.code FROM records AS r WHERE r.tenant IN (SELECT r.code FROM numeric_codes AS r WHERE r.code = $selected);`, 1},
		{"inner aggregate", ":many", `SELECT code FROM records WHERE tenant IN (SELECT COUNT(*) FROM allowed);`, 1},
		{"parenthesized scalar key", ":many", `SELECT code FROM records WHERE (tenant) IN (SELECT (tenant) FROM allowed);`, 1},
		{"nested membership", ":many", `SELECT code FROM records WHERE tenant IN (SELECT tenant FROM allowed WHERE tenant IN (SELECT code FROM numeric_codes));`, 1},
		{"scoped wildcards", ":many", `SELECT r.* FROM records AS r WHERE r.tenant IN (SELECT * FROM numeric_codes);`, 3},
		{"nullable key", ":many", `DECLARE $keys AS List<Struct<code:Utf8?>>; SELECT code FROM records WHERE code NOT IN (SELECT code FROM AS_TABLE($keys));`, 1},
		{"nullable tuple key", ":many", `DECLARE $keys AS List<Struct<tenant:Uint64,code:Utf8?>>; SELECT code FROM records WHERE (tenant,code) IN (SELECT (k.tenant,k.code) FROM AS_TABLE($keys) AS k);`, 1},
		{"tuple null component", ":many", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (tenant,NULL) FROM allowed);`, 1},
		{"scalar delete not in", ":exec", `DELETE FROM records WHERE tenant NOT IN (SELECT a.tenant FROM allowed AS a);`, 0},
		{"null projection", ":many", `SELECT code FROM records WHERE code IN (SELECT NULL FROM allowed);`, 1},
		{"null only membership", ":many", `SELECT code FROM records WHERE NULL IN (SELECT NULL FROM allowed);`, 1},
		{"null tuple components", ":many", `SELECT code FROM records WHERE (tenant,NULL) IN (SELECT (tenant,NULL) FROM allowed);`, 1},
		{"tuple projection alias", ":many", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (tenant,code) AS pair FROM allowed);`, 1},
		{"tuple implicit projection alias", ":many", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (tenant,code) pair FROM allowed);`, 1},
		{"computed keys", ":many", `SELECT code FROM records WHERE (tenant + 1ul, code || ""u) IN (SELECT (a.tenant, CAST(a.code AS Utf8)) FROM allowed AS a);`, 1},
		{"boolean nesting", ":many", `SELECT code FROM records WHERE NOT (tenant IN (SELECT code FROM numeric_codes)) OR (code IN (SELECT code FROM allowed) AND enabled);`, 1},
		{"delete predicate", ":exec", `DELETE FROM records WHERE (tenant, code) IN (SELECT (tenant, code) FROM allowed);`, 0},
		{"insert select predicate", ":exec", `INSERT INTO records (tenant, code, enabled) SELECT tenant, code, true FROM allowed WHERE tenant IN (SELECT code FROM numeric_codes);`, 0},
		{"update select predicate", ":exec", `UPDATE records ON SELECT tenant, code FROM allowed WHERE tenant IN (SELECT code FROM numeric_codes);`, 0},
		{"delete select predicate", ":exec", `DELETE FROM records ON SELECT tenant, code FROM allowed WHERE tenant IN (SELECT code FROM numeric_codes);`, 0},
		{"parameter lhs", ":many", `DECLARE $key AS Uint64; SELECT code FROM records WHERE $key IN (SELECT code FROM numeric_codes);`, 1},
		{"grouped subquery", ":many", `SELECT code FROM records WHERE tenant IN (SELECT tenant FROM allowed GROUP BY tenant HAVING COUNT(*) > 0ul);`, 1},
		{"inner output alias shadows outer", ":many", `SELECT code FROM records WHERE tenant IN (SELECT tenant AS enabled FROM allowed ORDER BY enabled);`, 1},
		{"tuple grouping", ":many", `SELECT code FROM records WHERE (tenant,code) IN (SELECT DISTINCT (tenant,code) FROM allowed GROUP BY tenant,code);`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := "-- name: Read " + tc.command + "\n" + tc.sql
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: inSubquerySchema}}, []model.Source{{Name: "query.sql", Text: sql}})
			if err != nil {
				t.Fatal(err)
			}
			q := result.Queries[0]
			if tc.columns == 0 {
				if len(q.ResultSets) != 0 {
					t.Fatalf("unexpected results: %+v", q.ResultSets)
				}
			} else if len(q.ResultSets) != 1 || len(q.ResultSets[0].Columns) != tc.columns {
				t.Fatalf("results: %+v", q.ResultSets)
			}
			if tc.name == "shadowed alias" {
				if len(q.Parameters) != 1 || q.Parameters[0].Type.Kind != "Uint64" || q.ResultSets[0].Columns[0].Type.Kind != "Utf8" {
					t.Fatalf("scope leaked: %+v", q)
				}
				if len(q.Syntax.Selects) != 2 || len(q.Syntax.Relations) != 1 {
					t.Fatalf("scopes=%+v, outer relations=%+v", q.Syntax.Selects, q.Syntax.Relations)
				}
			}
			if tc.name == "scoped wildcards" && strings.Contains(q.SQL, "*") {
				t.Fatalf("wildcard was not normalized: %s", q.SQL)
			}
		})
	}
}

func TestINSubqueryRejectsInvalidScopesAndKeys(t *testing.T) {
	for _, tc := range []struct{ name, sql, want string }{
		{"qualified correlation", `SELECT r.code FROM records AS r WHERE r.tenant IN (SELECT a.tenant FROM allowed AS a WHERE a.code = r.code);`, "correlated IN subqueries are unsupported"},
		{"unqualified correlation", `SELECT code FROM records WHERE tenant IN (SELECT code FROM numeric_codes WHERE enabled);`, "correlated IN subqueries are unsupported"},
		{"unknown inner", `SELECT code FROM records WHERE tenant IN (SELECT missing FROM allowed);`, "unknown column"},
		{"incompatible scalar", `SELECT code FROM records WHERE tenant IN (SELECT code FROM allowed);`, "IN subquery key types are incompatible"},
		{"tuple arity", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (tenant,code,tenant) FROM allowed);`, "IN subquery key types are incompatible"},
		{"tuple field type", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (code,tenant) FROM allowed);`, "IN subquery key types are incompatible"},
		{"multiple columns", `SELECT code FROM records WHERE (tenant,code) IN (SELECT tenant,code FROM allowed);`, "must return exactly one column"},
		{"unknown tuple component", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (missing,code) FROM allowed);`, "unknown column"},
		{"discard subquery", `SELECT code FROM records WHERE tenant IN (DISCARD SELECT tenant FROM allowed);`, "without DISCARD or INTO RESULT"},
		{"multiple wildcard columns", `SELECT code FROM records WHERE tenant IN (SELECT * FROM allowed);`, "must return exactly one column"},
		{"union", `SELECT code FROM records WHERE tenant IN (SELECT tenant FROM allowed UNION ALL SELECT code FROM numeric_codes);`, "CTEs, UNION and INTERSECT are unsupported"},
		{"derived source", `SELECT code FROM records WHERE tenant IN (SELECT tenant FROM (SELECT tenant FROM allowed));`, "only named catalog tables are supported"},
		{"shared external conflict", `SELECT r.code FROM records AS r WHERE r.code = $selected AND r.tenant IN (SELECT r.code FROM numeric_codes AS r WHERE r.code = $selected);`, "incompatible"},
		{"outer aggregate rejected", `SELECT code FROM records WHERE COUNT(*) IN (SELECT code FROM numeric_codes);`, "aggregate functions are not allowed in WHERE"},
		{"inner aggregate predicate rejected", `SELECT code FROM records WHERE tenant IN (SELECT tenant FROM allowed WHERE COUNT(*) > 0ul);`, "aggregate functions are not allowed in WHERE"},
		{"projection unsupported", `SELECT tenant IN (SELECT code FROM numeric_codes) AS value FROM records;`, "IN subqueries are supported only in WHERE predicates"},
		{"join predicate unsupported", `SELECT r.code FROM records AS r JOIN allowed AS a ON r.tenant = a.tenant AND r.code IN (SELECT code FROM allowed);`, "IN subqueries are supported only in WHERE predicates"},
		{"tuple outside IN", `SELECT code FROM records WHERE (tenant,code) IS NULL;`, "unsupported scalar expression"},
		{"tuple group violation", `SELECT code FROM records WHERE (tenant,code) IN (SELECT (tenant,code) FROM allowed GROUP BY tenant);`, "must appear in GROUP BY or an aggregate function"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: inSubquerySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestINSubqueryDatabaseDiscoveryAndPrefixes(t *testing.T) {
	sql := "-- name: Read :many\nPRAGMA TablePathPrefix = '/local/app'; SELECT id FROM outer_records WHERE id IN (SELECT id FROM inner_records VIEW by_id);"
	table := model.Table{Columns: []model.Column{{Name: "id", Type: model.Type{Kind: "Uint64"}}}, PrimaryKey: []string{"id"}}
	indexed := table
	indexed.Indexes = []model.Index{{Name: "by_id", Kind: "GlobalSync", Columns: []string{"id"}}}
	db := &fakeAnalysisDatabase{tables: map[string]model.Table{"/local/app/outer_records": table, "/local/app/inner_records": indexed}}
	result, err := AnalyzeWithDatabase(context.Background(), nil, []model.Source{{Name: "query.sql", Text: sql}}, Options{}, db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(db.described, []string{"/local/app/outer_records", "/local/app/inner_records"}) {
		t.Fatalf("described=%v", db.described)
	}
	if result.Queries[0].SQL != sql || len(result.Queries[0].Syntax.Selects) != 2 {
		t.Fatalf("query=%+v", result.Queries[0])
	}
}

func TestINSubquerySharesExternalParameterConstraints(t *testing.T) {
	for _, tc := range []struct{ name, command, sql string }{
		{"outer comparison to inner projection", ":many", `SELECT tenant FROM records WHERE tenant = $selected AND tenant IN (SELECT $selected);`},
		{"inner comparison to outer projection", ":many", `SELECT $selected AS selected FROM records WHERE tenant IN (SELECT tenant FROM allowed WHERE tenant = $selected);`},
		{"outer comparison to nested projection", ":many", `SELECT tenant FROM records WHERE tenant = $selected AND tenant IN (SELECT tenant FROM allowed WHERE tenant IN (SELECT $selected));`},
		{"update outer comparison", ":exec", `UPDATE records SET enabled = true WHERE tenant = $selected AND tenant IN (SELECT $selected);`},
		{"delete outer comparison", ":exec", `DELETE FROM records WHERE tenant = $selected AND tenant IN (SELECT $selected);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: inSubquerySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read " + tc.command + "\n" + tc.sql}})
			if err != nil {
				t.Fatal(err)
			}
			query := result.Queries[0]
			if len(query.Parameters) != 1 || query.Parameters[0].Name != "selected" || query.Parameters[0].Type.Kind != "Uint64" {
				t.Fatalf("parameters = %+v", query.Parameters)
			}
			if tc.command == ":many" && (len(query.ResultSets) != 1 || len(query.ResultSets[0].Columns) != 1 || query.ResultSets[0].Columns[0].Type.Kind != "Uint64") {
				t.Fatalf("results = %+v", query.ResultSets)
			}
		})
	}
}

func TestINSubqueryPreservesInnerTypeForSharedLimit(t *testing.T) {
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: `CREATE TABLE small_keys (id Uint32 NOT NULL, PRIMARY KEY(id));`}}, []model.Source{{Name: "query.sql", Text: `-- name: Read :many
SELECT id FROM small_keys WHERE id IN (SELECT id FROM small_keys WHERE id = $count) LIMIT $count;`}})
	if err != nil {
		t.Fatal(err)
	}
	parameters := result.Queries[0].Parameters
	if len(parameters) != 1 || parameters[0].Name != "count" || parameters[0].Type.Kind != "Uint32" {
		t.Fatalf("parameters = %+v", parameters)
	}
}

func TestINSubqueryPreservesUsefulDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, sql, want, absent string
	}{
		{
			name: "invalid inner predicate with shadowing output alias",
			sql: `SELECT code FROM records WHERE tenant IN (
    SELECT tenant + 1ul AS enabled FROM allowed WHERE code = 1ul ORDER BY enabled
);`,
			want:   "comparison operands have incompatible types",
			absent: "correlated IN subqueries",
		},
		{
			name:   "unknown outer key",
			sql:    `SELECT code FROM records WHERE missing_key IN (SELECT tenant FROM allowed);`,
			want:   `unknown column "missing_key"`,
			absent: "correlated IN subqueries",
		},
		{
			name:   "named tuple projection",
			sql:    `SELECT code FROM records WHERE (tenant, code) IN (SELECT (tenant, code AS key) FROM allowed);`,
			want:   `computed result expression "(tenant,codeASkey)" is not supported`,
			absent: "correlated IN subqueries",
		},
		{
			name: "empty tuple key",
			sql:  `SELECT code FROM records WHERE () IN (SELECT tenant FROM allowed);`,
			want: `unsupported scalar expression "()"`,
		},
		{
			name: "empty parenthesized IN operand",
			sql:  `SELECT code FROM records WHERE tenant IN ();`,
			want: `unsupported IN operand "()"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Analyze([]model.Source{{Name: "schema.sql", Text: inSubquerySchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Read :many\n" + tc.sql}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if tc.absent != "" && strings.Contains(err.Error(), tc.absent) {
				t.Fatalf("error = %v must not contain %q", err, tc.absent)
			}
		})
	}
}
