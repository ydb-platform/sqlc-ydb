package analyzer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const computedDMLSchema = `CREATE TABLE counters (
    id Utf8 NOT NULL, value Int64 NOT NULL, optional_value Int64,
    label Utf8, enabled Bool NOT NULL, PRIMARY KEY (id)
);`

func TestAnalyzeComputedDMLValues(t *testing.T) {
	for _, tt := range []struct {
		name, sql, parameters string
	}{
		{"insert literals", `INSERT INTO counters (id, value, label, enabled) VALUES ($id, 0, 'pending'u, true);`, "id"},
		{"upsert rows", `UPSERT INTO counters (id, value, label, enabled) VALUES ($id, 1l + 2 * 3, NULL, false), ('other'u, (10l - 2) * 3, 'ready'u, true);`, "id"},
		{"increment", `DECLARE $delta AS Int64; UPDATE counters SET value = value + $delta WHERE id = $id;`, "delta,id"},
		{"precedence", `UPDATE counters SET value = (value + 2) * 3 - 4, label = 'done'u, enabled = false WHERE id = $id;`, "id"},
		{"nullable arithmetic", `UPDATE counters SET optional_value = optional_value + 1, label = NULL WHERE id = $id;`, "id"},
		{"cast", `UPDATE counters SET value = CAST(1u AS Int64) WHERE id = $id;`, "id"},
		{"case", `UPDATE counters SET value = CASE WHEN enabled THEN value + 1 ELSE 0l END WHERE id = $id;`, "id"},
		{"unsigned widening", `UPDATE counters SET optional_value = 1u WHERE id = $id;`, "id"},
		{"boolean comparison", `UPDATE counters SET enabled = value > 1 WHERE id = $id;`, "id"},
		{"parenthesized comparison", `UPDATE counters SET enabled = ((value > 1)) WHERE id = $id;`, "id"},
		{"insert comparison", `INSERT INTO counters (id, value, enabled) VALUES ($id, 0, (2 > 1));`, "id"},
		{"column copy", `UPDATE counters SET optional_value = value WHERE id = $id;`, "id"},
		{"scalar call", `UPDATE counters SET value = COALESCE(optional_value, 0l) + 1 WHERE id = $id;`, "id"},
		{"local binding", `$step = 2l; UPDATE counters SET value = value + $step WHERE id = $id;`, "id"},
		{"parenthesized parameter", `UPDATE counters SET value = (($value)) WHERE id = $id;`, "value,id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sql := "-- name: Write :one\n" + strings.TrimSuffix(tt.sql, ";") + " RETURNING value, optional_value;"
			input := []model.Source{{Name: "query.sql", Text: sql}}
			offline, err := Analyze([]model.Source{{Name: "schema.sql", Text: computedDMLSchema}}, input)
			require.NoError(t, err)
			query := offline.Queries[0]
			require.Equal(t, sql, query.SQL)
			want := []model.Column{
				{Name: "value", Type: model.Type{Kind: "Int64"}, Table: "counters"},
				{Name: "optional_value", Type: model.Optional(model.Type{Kind: "Int64"}), Table: "counters"},
			}
			require.Equal(t, want, query.ResultSets[0].Columns)
			parameterNames := strings.Split(tt.parameters, ",")
			require.Len(t, query.Parameters, len(parameterNames))
			for i, parameter := range query.Parameters {
				require.Equal(t, parameterNames[i], parameter.Name)
				kind := "Int64"
				if parameter.Name == "id" {
					kind = "Utf8"
				}
				require.NotEqual(t, "step", parameter.Name)
				require.Equal(t, kind, parameter.Type.Kind)
			}
			db := &fakeAnalysisDatabase{tables: map[string]model.Table{"counters": offline.Catalog.Tables[0]}}
			connected, err := AnalyzeWithDatabase(context.Background(), nil, input, Options{}, db)
			require.NoError(t, err)
			require.Equal(t, query.Parameters, connected.Queries[0].Parameters)
			require.Equal(t, query.ResultSets, connected.Queries[0].ResultSets)
		})
	}
}

func TestAnalyzeRejectsInvalidDMLExpressions(t *testing.T) {
	for _, tt := range []struct{ name, sql, want string }{
		{"wrong literal", `UPDATE counters SET value = 'wrong'u;`, "cannot assign Utf8"},
		{"required null", `UPDATE counters SET value = NULL;`, "cannot assign Null"},
		{"optional to required", `UPDATE counters SET value = optional_value + 1;`, "cannot assign Optional<Int64>"},
		{"wrong operand", `UPDATE counters SET value = value + 'wrong'u;`, "arithmetic"},
		{"undeclared operand", `UPDATE counters SET value = value + $delta;`, "DECLARE"},
		{"unsupported division", `UPDATE counters SET value = value / 2;`, "unsupported arithmetic operator"},
		{"insert column reference", `INSERT INTO counters (id, value) VALUES ('a'u, value);`, "column references are not allowed in VALUES"},
		{"nested VALUES column reference", `UPSERT INTO counters (id, value) VALUES ('a'u, COALESCE(optional_value, 0l) + 1);`, "column references are not allowed in VALUES"},
		{"duplicate SET target", `UPDATE counters SET value = value + 1, value = value + 2;`, `duplicate UPDATE SET column "value"`},
		{"duplicate quoted SET target", "UPDATE counters SET value = 1, `value` = 2;", `duplicate UPDATE SET column "value"`},
		{"nullable comparison", `UPDATE counters SET enabled = (optional_value > 1);`, "cannot assign Optional<Bool>"},
		{"comparison to integer", `UPDATE counters SET value = (value > 1);`, "cannot assign Bool"},
		{"aggregate", `UPDATE counters SET value = SUM(value);`, "aggregate"},
		{"window", `UPDATE counters SET value = SUM(value) OVER ();`, "aggregate"},
		{"tuple", `UPDATE counters SET value = (1, 2);`, "unsupported"},
		{"subquery", `UPDATE counters SET value = (SELECT 1);`, "unsupported"},
		{"narrowing", `UPDATE counters SET value = 1ul;`, "cannot assign Uint64"},
		{"float assignment", `UPDATE counters SET value = 1.0;`, "cannot assign Double"},
		{"unknown function", `UPDATE counters SET value = Unknown::Value();`, "unsupported YQL function"},
		{"lambda", `UPDATE counters SET value = ($x) -> { RETURN $x; };`, "unsupported"},
		{"unknown column", `UPDATE counters SET value = missing + 1;`, "unknown column"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Analyze([]model.Source{{Name: "schema.sql", Text: computedDMLSchema}}, []model.Source{{Name: "query.sql", Text: "-- name: Write :exec\n" + tt.sql}})
			require.ErrorContains(t, err, tt.want)
			require.NotEqual(t, 0, len(result.Diagnostics))
			require.Equal(t, "query.sql", result.Diagnostics[0].Position.File)
		})
	}
}
