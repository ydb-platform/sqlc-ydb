package database

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Query"

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
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
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
		if err != nil {
			t.Fatal(err)
		}
		var parts []*Ydb_Query.ExecuteQueryResponsePart
		for {
			part, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			parts = append(parts, part)
		}
		if len(parts) == 0 {
			t.Fatal("server returned no response parts")
		}
		return parts
	}
	checkColumns := func(t *testing.T, parts []*Ydb_Query.ExecuteQueryResponsePart, want []string) {
		t.Helper()
		var columns []string
		for _, part := range parts {
			if err := statusError(part.GetStatus(), part.GetIssues()); err != nil {
				t.Fatal(err)
			}
			if result := part.GetResultSet(); result != nil {
				if len(result.GetRows()) != 0 {
					t.Fatalf("LIMIT 0 returned %d rows", len(result.GetRows()))
				}
				for _, column := range result.GetColumns() {
					typ, err := decodeType(column.GetType())
					if err != nil {
						t.Fatal(err)
					}
					columns = append(columns, column.GetName()+":"+typ.String())
				}
			}
		}
		if !reflect.DeepEqual(columns, want) {
			t.Fatalf("result columns: got %v, want %v", columns, want)
		}
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
		t.Fatalf("expected %s with %q; responses: %v", wantStatus, wantMessage, parts)
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
		checkColumns(t, parts, []string{"next_id:Uint64", "label:Optional<Utf8>", "numbers:List<Int32>", "amount:Optional<Decimal(22,9)>", "total:Uint64"})
	})
	t.Run("declared_explain_without_values", func(t *testing.T) {
		for _, part := range run(t, declaredSQL, Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN, nil) {
			if err := statusError(part.GetStatus(), part.GetIssues()); err != nil {
				t.Fatal(err)
			}
			if part.GetResultSet() != nil {
				t.Fatalf("EXPLAIN returned result metadata; reassess compile-only type discovery: %v", part.GetResultSet())
			}
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
		checkColumns(t, run(t, limitSQL, Ydb_Query.ExecMode_EXEC_MODE_EXECUTE, parameters), []string{"incremented:Uint64", "label:Optional<Utf8>", "total:Uint64"})
	})
}
