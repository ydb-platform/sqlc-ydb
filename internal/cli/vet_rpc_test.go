package cli

import (
	"net"
	"testing"
	"time"

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
}

func (s vetPlanServer) ExecuteQuery(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
	for _, part := range s.parts {
		if err := stream.Send(part); err != nil {
			return err
		}
	}
	return nil
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
