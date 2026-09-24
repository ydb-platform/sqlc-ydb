package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Query"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

// This opt-in protocol probe executes only fixed, table-free SELECT fixtures.
// It does not add an execution path to database-assisted query analysis.
func TestLiveYDBQueryMetadata(t *testing.T) {
	uri := os.Getenv("YDB_CONNECTION_STRING")
	if uri == "" {
		t.Skip("set YDB_CONNECTION_STRING for live query metadata probes")
	}
	settings, err := (config.Database{URI: uri, Timeout: "30s"}).Resolve(".")
	require.NoError(t, err)
	client, err := New(settings)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, client.Close())
	})

	run := func(t *testing.T, sql string, mode Ydb_Query.ExecMode, parameters map[string]*Ydb.TypedValue) []*Ydb_Query.ExecuteQueryResponsePart {
		t.Helper()
		ctx, cancel := client.requestContext(context.Background())
		defer cancel()
		request := &Ydb_Query.ExecuteQueryRequest{
			ExecMode: mode,
			Query: &Ydb_Query.ExecuteQueryRequest_QueryContent{QueryContent: &Ydb_Query.QueryContent{
				Syntax: Ydb_Query.Syntax_SYNTAX_YQL_V1, Text: sql,
			}},
			Parameters: parameters,
		}
		if mode == Ydb_Query.ExecMode_EXEC_MODE_EXECUTE {
			request.TxControl = &Ydb_Query.TransactionControl{
				TxSelector: &Ydb_Query.TransactionControl_BeginTx{BeginTx: &Ydb_Query.TransactionSettings{
					TxMode: &Ydb_Query.TransactionSettings_SnapshotReadOnly{SnapshotReadOnly: &Ydb_Query.SnapshotModeSettings{}},
				}},
				CommitTx: true,
			}
		}
		stream, err := client.queries.ExecuteQuery(ctx, request)
		require.NoError(t, err)
		var parts []*Ydb_Query.ExecuteQueryResponsePart
		for {
			part, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			parts = append(parts, part)
		}
		require.NotEqual(t, 0, len(parts), "server returned no response parts")
		return parts
	}
	checkColumns := func(t *testing.T, parts []*Ydb_Query.ExecuteQueryResponsePart, want []string, wantRows int) {
		t.Helper()
		var columns []string
		rows := 0
		for _, part := range parts {
			require.NoError(t, statusError(part.GetStatus(), part.GetIssues()))
			if result := part.GetResultSet(); result != nil {
				rows += len(result.GetRows())
				for _, column := range result.GetColumns() {
					typ, err := decodeType(column.GetType())
					require.NoError(t, err)
					columns = append(columns, column.GetName()+":"+typ.String())
				}
			}
		}
		require.Equal(t, wantRows, rows, "result row count: got %d, want %d", rows, wantRows)
		require.True(t, reflect.DeepEqual(columns, want), "result columns: got %v, want %v", columns, want)
	}
	checkError := func(t *testing.T, parts []*Ydb_Query.ExecuteQueryResponsePart, wantStatus Ydb.StatusIds_StatusCode, wantMessage string) {
		t.Helper()
		for _, part := range parts {
			if part.GetStatus() == wantStatus {
				err := statusError(part.GetStatus(), part.GetIssues())
				if err != nil && strings.Contains(err.Error(), wantMessage) {
					return
				}
			}
		}
		require.FailNow(t, fmt.Sprintf("expected %s with %q; responses: %v", wantStatus, wantMessage, parts))
	}

	const complexSQL = `SELECT 1ul + 2ul AS next_id, CAST(NULL AS Utf8?) AS label,
    [1, 2] AS numbers, CAST('12.34' AS Decimal(22,9)) AS amount,
    COUNT(*) OVER () AS total FROM AS_TABLE([<|dummy: 1|>]) LIMIT 0;`
	const declaredSQL = `DECLARE $id AS Uint64;
SELECT $id + 1ul AS incremented, CAST(NULL AS Utf8?) AS label,
    COUNT(*) OVER () AS total FROM AS_TABLE([<|dummy: 1|>]);`
	limitSQL := strings.TrimSuffix(declaredSQL, ";") + " LIMIT 0;"

	t.Run("complex_limit_zero_metadata", func(t *testing.T) {
		parts := run(t, complexSQL, Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, nil)
		checkColumns(t, parts, []string{"next_id:Uint64", "label:Optional<Utf8>", "numbers:List<Int32>", "amount:Optional<Decimal(22,9)>", "total:Uint64"}, 0)
	})
	t.Run("declared_explain_without_values", func(t *testing.T) {
		for _, part := range run(t, declaredSQL, Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN, nil) {
			require.NoError(t, statusError(part.GetStatus(), part.GetIssues()))
			require.Nil(t, part.GetResultSet(), "EXPLAIN returned result metadata; reassess compile-only type discovery")
		}
	})
	t.Run("undeclared_explain_error", func(t *testing.T) {
		parts := run(t, "SELECT $id + 1ul AS incremented LIMIT 0;", Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN, nil)
		checkError(t, parts, Ydb.StatusIds_GENERIC_ERROR, "Unknown name: $id")
	})
	t.Run("declared_limit_zero_requires_values", func(t *testing.T) {
		parts := run(t, limitSQL, Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, nil)
		checkError(t, parts, Ydb.StatusIds_BAD_REQUEST, "Missing value for parameter: $id")
	})
	t.Run("declared_limit_zero_with_explicit_fixture_value", func(t *testing.T) {
		parameters := map[string]*Ydb.TypedValue{"$id": {
			Type:  &Ydb.Type{Type: &Ydb.Type_TypeId{TypeId: Ydb.Type_UINT64}},
			Value: &Ydb.Value{Value: &Ydb.Value_Uint64Value{Uint64Value: 42}},
		}}
		checkColumns(t, run(t, limitSQL, Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, parameters), []string{"incremented:Uint64", "label:Optional<Utf8>", "total:Uint64"}, 0)
	})
	t.Run("queryservice_structured_parameters_limit_zero_and_one", func(t *testing.T) {
		const sql = `DECLARE $id AS Uint64;
DECLARE $text AS Utf8;
DECLARE $maybe AS Int32?;
DECLARE $rows AS List<Struct<key:Uint64,label:Utf8?>>;
SELECT $id + 1ul AS incremented, $text AS label, $maybe AS maybe,
    r.key AS row_key, r.label AS row_label, COUNT(*) OVER () AS total
FROM AS_TABLE($rows) AS r ORDER BY row_key`
		for _, part := range run(t, sql+" LIMIT 1;", Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN, nil) {
			require.NoError(t, statusError(part.GetStatus(), part.GetIssues()))
		}
		types := map[string]*Ydb.Type{
			"$id":    primitive(Ydb.Type_UINT64),
			"$text":  primitive(Ydb.Type_UTF8),
			"$maybe": optional(primitive(Ydb.Type_INT32)),
			"$rows": {Type: &Ydb.Type_ListType{ListType: &Ydb.ListType{Item: &Ydb.Type{Type: &Ydb.Type_StructType{StructType: &Ydb.StructType{Members: []*Ydb.StructMember{
				{Name: "key", Type: primitive(Ydb.Type_UINT64)},
				{Name: "label", Type: optional(primitive(Ydb.Type_UTF8))},
			}}}}}}},
		}
		values := map[string]*Ydb.Value{
			"$id":    {Value: &Ydb.Value_Uint64Value{Uint64Value: 42}},
			"$text":  {Value: &Ydb.Value_TextValue{TextValue: "example"}},
			"$maybe": {Value: &Ydb.Value_NullFlagValue{NullFlagValue: structpb.NullValue_NULL_VALUE}},
			"$rows": {Items: []*Ydb.Value{{Items: []*Ydb.Value{
				{Value: &Ydb.Value_Uint64Value{Uint64Value: 9}},
				{Value: &Ydb.Value_NullFlagValue{NullFlagValue: structpb.NullValue_NULL_VALUE}},
			}}}},
		}
		parameters := make(map[string]*Ydb.TypedValue, len(types))
		for name, typ := range types {
			parameters[name] = &Ydb.TypedValue{Type: typ, Value: values[name]}
		}
		want := []string{"incremented:Uint64", "label:Utf8", "maybe:Optional<Int32>", "row_key:Uint64", "row_label:Optional<Utf8>", "total:Uint64"}
		for _, limit := range []int{0, 1} {
			parts := run(t, sql+fmt.Sprintf(" LIMIT %d;", limit), Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, parameters)
			checkColumns(t, parts, want, limit)
		}
	})

	t.Run("limit_zero_omits_value_dependent_runtime_check", func(t *testing.T) {
		const sql = "DECLARE $id AS Uint64; SELECT Ensure($id, $id > 0ul, 'id must be positive') AS id"
		parameters := map[string]*Ydb.TypedValue{"$id": {
			Type: primitive(Ydb.Type_UINT64), Value: &Ydb.Value{Value: &Ydb.Value_Uint64Value{Uint64Value: 0}},
		}}
		checkColumns(t, run(t, sql+" LIMIT 0;", Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, parameters), []string{"id:Uint64"}, 0)
		checkError(t, run(t, sql+" LIMIT 1;", Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, parameters), Ydb.StatusIds_PRECONDITION_FAILED, "id must be positive")
	})

}
