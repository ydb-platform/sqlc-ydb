package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

func TestListLambdaScopes(t *testing.T) {
	for _, tc := range []struct {
		name, sql, want string
	}{
		{"inline", `SELECT ListMap(ListCreate(Uint64), ($x) -> ($x + 1ul)) AS values;`, "List<Uint64>"},
		{"block local", `SELECT ListFilter(ListCreate(Uint64), ($x) -> { $limit = $x + 1ul; RETURN $x < $limit; }) AS values;`, "List<Uint64>"},
		{"named local", `$advance = ($x) -> ($x + 1ul); SELECT ListMap(ListCreate(Uint64), $advance) AS values;`, "List<Uint64>"},
		{"named capture", `DECLARE $step AS Uint64; $advance = ($x) -> ($x + $step); SELECT ListMap(ListCreate(Uint64), $advance) AS values;`, "List<Uint64>"},
		{"named invocation", `$advance = ($x) -> ($x + 1ul); SELECT $advance(2ul) AS values;`, "Uint64"},
		{"named alias", `$advance = ($x) -> ($x + 1ul); $next = $advance; SELECT $next(2ul) AS values;`, "Uint64"},
		{"named in tabular local", `$advance = ($x) -> ($x + 1ul); $rows = (SELECT $advance(1ul) AS n); SELECT * FROM $rows;`, "Uint64"},
		{"nested and captured", `DECLARE $offset AS Uint64; SELECT ListMap(ListCreate(Uint64), ($x) -> (ListMap(ListCreate(Uint64), ($y) -> ($x + $y + $offset)))) AS values;`, "List<List<Uint64>>"},
		{"nested shadow and local capture", `SELECT ListMap(ListCreate(Uint64), ($x) -> { $y = $x + 1ul; RETURN ListMap(ListCreate(Uint64), ($x) -> ($x + $y)); }) AS values;`, "List<List<Uint64>>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Queries[0].ResultSets[0].Columns[0].Type.String())
			if tc.name == "nested and captured" {
				require.Equal(t, []model.Parameter{{Name: "offset", Type: model.Type{Kind: "Uint64"}}}, result.Queries[0].Parameters)
			} else if tc.name == "named capture" {
				require.Equal(t, []model.Parameter{{Name: "step", Type: model.Type{Kind: "Uint64"}}}, result.Queries[0].Parameters)
			} else {
				require.Empty(t, result.Queries[0].Parameters)
			}
		})
	}
}

func TestListLambdaDiagnostics(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT ListMap(ListCreate(Uint64), ($x, $y) -> ($x)) AS values;`, "expects 1 parameter"},
		{`SELECT ListMap(ListCreate(Uint64), ($x + 1ul) -> ($x)) AS values;`, "parameters must be plain"},
		{`SELECT ListMap(ListCreate(Uint64), ($x) -> ($missing)) AS values;`, "cannot resolve type of parameter $missing"},
		{`SELECT ListMap(ListCreate(Uint64), ($x) -> { $y = $x; $y = $x; RETURN $y; }) AS values;`, "assigned more than once"},
		{`SELECT ListMap(ListCreate(Uint64), ($x) -> (COUNT($x))) AS values;`, "aggregate functions are not allowed in lambda"},
		{`$f = ($x) -> ($x); SELECT $f AS value;`, "nonpersistable type Lambda"},
		{`$f = ($x) -> ($x); SELECT ListMap(ListCreate(Uint64), ($f) -> (ListMap(ListCreate(Uint64), $f))) AS value;`, "argument 2 must be a one-argument Callable"},
		{`$f = ($x) -> ($x); SELECT ListMap(ListCreate(Uint64), ($x) -> { $f = 1ul; RETURN ListMap(ListCreate(Uint64), $f); }) AS value;`, "argument 2 must be a one-argument Callable"},
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
		require.ErrorContains(t, err, tc.want)
	}
}

func TestLambdaRejectsTableColumnCapture(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE t (id Uint64 NOT NULL, PRIMARY KEY(id));`}}
	for _, sql := range []string{
		`$f = ($x) -> (id + $x); SELECT $f(1ul) AS value FROM t;`,
		`SELECT ListMap(ListCreate(Uint64), ($x) -> (id + $x)) AS value FROM t;`,
		`SELECT ListMap(ListCreate(Uint64), ($x) -> (<| value: id + $x |>)) AS value FROM t;`,
	} {
		_, err := Analyze(schema, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + sql}})
		require.ErrorContains(t, err, `column reference "id" is not allowed in lambda function`)
	}
}

func TestLambdaRejectsHigherOrderParameter(t *testing.T) {
	for _, sql := range []string{
		`$f = ($x) -> ($x + 1ul); $apply = ($f) -> (ListMap(AsList(1ul), $f)); $other = ($x) -> ("hello"u); SELECT $apply($other) AS value;`,
		`$f = ($x) -> ($x + 1ul); $apply = ($f) -> ($f(1ul)); $other = ($x) -> ("hello"u); SELECT $apply($other) AS value;`,
	} {
		_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + sql}})
		require.ErrorContains(t, err, "lambda-valued parameter $f is unsupported")
	}
}

func TestDocumentFromContextualEmptyList(t *testing.T) {
	for _, tc := range []struct{ sql, want string }{
		{`SELECT Yson::SerializeJson(Json::From(AsList())) AS doc;`, "Optional<Json>"},
		{`SELECT Yson::SerializeJson(Yson::From(AsList())) AS doc;`, "Optional<Json>"},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			result, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\n" + tc.sql}})
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Queries[0].ResultSets[0].Columns[0].Type.String())
		})
	}
	_, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: "-- name: Value :one\nSELECT AsList() AS values;"}})
	require.ErrorContains(t, err, "empty List with no element type")
}

func TestNamedLambdaStructResultAndMemberAccess(t *testing.T) {
	sql := `-- name: Status :one
$parse = ($raw) -> {
    $stored = Yson::ConvertTo(Yson::ParseJson($raw), Struct<status:String?>);
    RETURN <| status: COALESCE($stored.status, "unknown") |>;
};
$get = ($raw) -> (($parse($raw)).status);
SELECT $get("{\"status\":\"ready\"}") AS value;`
	got, err := Analyze(nil, []model.Source{{Name: "query.sql", Text: sql}})
	require.NoError(t, err)
	require.Equal(t, "String", got.Queries[0].ResultSets[0].Columns[0].Type.String())
}

func TestNamedLambdaTakesUpdateColumnArgument(t *testing.T) {
	schema := []model.Source{{Name: "schema.sql", Text: `CREATE TABLE devices (id String NOT NULL, status Json NOT NULL, PRIMARY KEY(id));`}}
	query := []model.Source{{Name: "query.sql", Text: `-- name: UpdateStatus :exec
DECLARE $id AS String;
$identity = ($raw) -> ($raw);
UPDATE devices SET status = $identity(status) WHERE id = $id;`}}
	got, err := Analyze(schema, query)
	require.NoError(t, err)
	require.Len(t, got.Queries[0].Parameters, 1)
	require.Equal(t, "id", got.Queries[0].Parameters[0].Name)
}
