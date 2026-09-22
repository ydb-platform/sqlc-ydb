package analyzer

import (
	"strings"
	"testing"

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
			if err == nil {
				t.Fatal("accepted a non-scalar expression as scalar parentheses")
			}
			if !strings.Contains(err.Error(), "unsupported result expression") && !strings.Contains(err.Error(), "computed result expression") {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
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
	if err != nil {
		t.Fatal(err)
	}
	if result.Queries[0].SQL != sql {
		t.Fatal("nested arithmetic SQL changed")
	}
	_, err = Analyze([]model.Source{{Name: "schema.sql", Text: computedDMLSchema}}, []model.Source{{Name: "q.sql", Text: strings.Replace(sql, "value + 1l", "missing + 1l", 1)}})
	if err == nil || !strings.Contains(err.Error(), `unknown column "missing"`) {
		t.Fatalf("lost nested column validation: %v", err)
	}
}
