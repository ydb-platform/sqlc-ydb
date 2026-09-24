package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestAnalyzeRejectsNonScalarParentheses(t *testing.T) {
	for _, expression := range []string{
		"(WITH x AS (SELECT 1 AS v) SELECT v FROM x)",
		"(1 UNION ALL 2)",
		"(1 INTERSECT 2)",
		"(1 EXCEPT 2)",
		"(1 AS named)",
		"(1,)",
		"(1, 2)",
		"()",
		"(SELECT 1)",
		"($x) -> { RETURN $x; }",
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := Analyze(nil, []model.Source{{Name: "q.sql", Text: "-- name: Read :one\nSELECT " + expression + " AS value;"}})
			require.Error(t, err)
			require.False(t, !strings.Contains(err.Error(), "unsupported result expression") && !strings.Contains(err.Error(), "computed result expression"))
		})
	}
}

func TestAnalyzeNestedArithmeticPreservesScopeAndTypes(t *testing.T) {
	expression := "value"
	for range 32 {
		expression = "(" + expression + " + 1l) * 2l"
	}
	sql := "-- name: Write :exec\nUPDATE counters SET value = " + expression + ";"
	result, err := Analyze([]model.Source{{Name: "schema.sql", Text: computedDMLSchema}}, []model.Source{{Name: "q.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, sql, result.Queries[0].SQL)
	_, err = Analyze([]model.Source{{Name: "schema.sql", Text: computedDMLSchema}}, []model.Source{{Name: "q.sql", Text: strings.Replace(sql, "value + 1l", "missing + 1l", 1)}})
	require.ErrorContains(t, err, `unknown column "missing"`)
}
