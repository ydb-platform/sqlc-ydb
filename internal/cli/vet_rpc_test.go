package cli

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"
	queryservice "github.com/ydb-platform/ydb-go-genproto/Ydb_Query_V1"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Query"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_TableStats"
	"google.golang.org/grpc"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/database"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

type vetPlanServer struct {
	queryservice.UnimplementedQueryServiceServer
	parts []*Ydb_Query.ExecuteQueryResponsePart
	calls *atomic.Int32
}

func (s vetPlanServer) ExecuteQuery(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
	if s.calls != nil {
		s.calls.Add(1)
	}
	for _, part := range s.parts {
		if err := stream.Send(part); err != nil {
			return err
		}
	}
	return nil
}

func TestVetConnectedPlanAndPrepareRules(t *testing.T) {
	const plan = `{"Plan":{"Node Type":"Query","Operators":[{"Name":"TableFullScan"}]}}`
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	var calls atomic.Int32
	queryservice.RegisterQueryServiceServer(server, vetPlanServer{parts: []*Ydb_Query.ExecuteQueryResponsePart{{Status: Ydb.StatusIds_SUCCESS, ExecStats: &Ydb_TableStats.QueryStats{QueryPlan: plan}}}, calls: &calls})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	dir := t.TempDir()
	put(t, filepath.Join(dir, "queries.sql"), "-- name: One :one\nSELECT 1 AS n;")
	configPath := filepath.Join(dir, "sqlc.yaml")
	configuration := fmt.Sprintf("version: '2'\nsql:\n- engine: ydb\n  queries: queries.sql\n  database:\n    uri: grpc://%s/local\n  rules: [sqlc/db-prepare, plan-check]\nrules:\n- name: plan-check\n  rule: ydb.plan.operations.exists(op, op == 'TableFullScan')\n", listener.Addr())
	put(t, configPath, configuration)
	code, _, stderr := invoke("vet", "-f", configPath)
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "query One: vet rule plan-check: rule matched")
	require.EqualValues(t, 2, calls.Load())
	put(t, configPath, strings.Replace(configuration, "[sqlc/db-prepare, plan-check]", "[sqlc/db-prepare]", 1))
	code, _, stderr = invoke("vet", "-f", configPath)
	require.Zero(t, code, stderr)
	require.EqualValues(t, 3, calls.Load())
}

func TestVetRejectsMissingOrMalformedYDBPlan(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		part       *Ydb_Query.ExecuteQueryResponsePart
	}{
		{"missing plan", "server returned no query plan", &Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS}},
		{"malformed plan", "decode YDB query plan", &Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS, ExecStats: &Ydb_TableStats.QueryStats{QueryPlan: "not-json"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			server := grpc.NewServer()
			queryservice.RegisterQueryServiceServer(server, vetPlanServer{parts: []*Ydb_Query.ExecuteQueryResponsePart{tc.part}})
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			client, err := database.New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Timeout: time.Second})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			failures, err := vetQueries(&config.Config{Version: "2"}, config.SQL{Engine: "ydb", Rules: []string{"plan-check"}}, []model.AnalyzedQuery{{Name: "List", SQL: "SELECT 1;"}}, map[string]vetRule{"plan-check": {needsPlan: true}}, client)
			require.Empty(t, failures)
			require.ErrorContains(t, err, "query List:")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestVetEvaluatesPlanActivationOffline(t *testing.T) {
	const plan = `{"Plan":{"Node Type":"TableFullScan"}}`
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	queryservice.RegisterQueryServiceServer(server, vetPlanServer{parts: []*Ydb_Query.ExecuteQueryResponsePart{{Status: Ydb.StatusIds_SUCCESS, ExecStats: &Ydb_TableStats.QueryStats{QueryPlan: plan}}}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := database.New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	env, err := cel.NewEnv(cel.Variable("ydb", cel.DynType))
	require.NoError(t, err)
	ast, issues := env.Compile(`ydb.plan.operations.exists(op, op == 'TableFullScan') && ydb.plan.json.contains('TableFullScan') && ydb.explain.Plan['Node Type'] == 'TableFullScan'`)
	require.NoError(t, issues.Err())
	program, err := env.Program(ast)
	require.NoError(t, err)
	failures, err := vetQueries(&config.Config{Version: "2"}, config.SQL{Engine: "ydb", Rules: []string{"plan-check"}}, []model.AnalyzedQuery{{Name: "List", SQL: "SELECT 1;"}}, map[string]vetRule{"plan-check": {program: program, needsPlan: true}}, client)
	require.NoError(t, err)
	require.Len(t, failures, 1)
	require.ErrorContains(t, failures[0], "query List: vet rule plan-check: rule matched")
}
