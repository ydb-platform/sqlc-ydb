package database

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	queryservice "github.com/ydb-platform/ydb-go-genproto/Ydb_Query_V1"
	tableservice "github.com/ydb-platform/ydb-go-genproto/Ydb_Table_V1"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Issue"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Operations"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Query"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Table"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
)

type tableServer struct {
	tableservice.UnimplementedTableServiceServer
	describe func(context.Context, *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error)
}

func (s tableServer) DescribeTable(ctx context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
	return s.describe(ctx, request)
}

type queryServer struct {
	queryservice.UnimplementedQueryServiceServer
	explain func(*Ydb_Query.ExecuteQueryRequest, queryservice.QueryService_ExecuteQueryServer) error
}

func (s queryServer) ExecuteQuery(request *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
	return s.explain(request, stream)
}

func testClient(t *testing.T, table tableServer, query queryServer) *Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	tableservice.RegisterTableServiceServer(server, table)
	queryservice.RegisterQueryServiceServer(server, query)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///database-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &Client{conn: conn, tables: tableservice.NewTableServiceClient(conn), queries: queryservice.NewQueryServiceClient(conn), database: "/local", token: "test-token", timeout: time.Second}
}

func TestDescribeTableSessionlessMetadata(t *testing.T) {
	client := testClient(t, tableServer{describe: func(ctx context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
		checkHeadersAndDeadline(t, ctx)
		assert.Equal(t, "/local/items", request.GetPath())
		assert.Empty(t, request.GetSessionId())
		assert.Equal(t, Ydb_Operations.OperationParams_SYNC, request.GetOperationParams().GetOperationMode())
		result, err := anypb.New(&Ydb_Table.DescribeTableResult{Columns: []*Ydb_Table.ColumnMeta{
			{Name: "id", Type: primitive(Ydb.Type_UINT64), DefaultValue: &Ydb_Table.ColumnMeta_FromSequence{FromSequence: &Ydb_Table.SequenceDescription{Name: proto.String("_serial_column_id")}}},
			{Name: "title", Type: optional(primitive(Ydb.Type_UTF8))},
		}, PrimaryKey: []string{"id"}})
		if err != nil {
			return nil, err
		}
		return &Ydb_Table.DescribeTableResponse{Operation: &Ydb_Operations.Operation{Ready: true, Status: Ydb.StatusIds_SUCCESS, Result: result}}, nil
	}}, queryServer{})
	table, err := client.DescribeTable(context.Background(), "items")
	require.NoError(t, err)
	require.Equal(t, "items", table.Name)
	require.Len(t, table.Columns, 2)
	require.Equal(t, "items", table.Columns[0].Table)
	require.True(t, table.Columns[0].SequenceGenerated)
	require.Equal(t, "Optional<Utf8>", table.Columns[1].Type.String())
	require.Equal(t, []string{"id"}, table.PrimaryKey)
}

func TestDescribeTablePathResolution(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		wantError  bool
	}{
		{"../outside", "", true},
		{"folder/../../outside", "", true},
		{"folder/../items", "", true},
		{"items/..", "", true},
		{"..", "", true},
		{"folder/items", "/local/folder/items", false},
		{"./folder/items", "/local/folder/items", false},
		{"folder/..backup/items", "/local/folder/..backup/items", false},
		{"/other/items", "/other/items", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan string, 1)
			client := testClient(t, tableServer{describe: func(_ context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
				requests <- request.GetPath()
				result, err := anypb.New(&Ydb_Table.DescribeTableResult{
					Columns:    []*Ydb_Table.ColumnMeta{{Name: "id", Type: primitive(Ydb.Type_UINT64)}},
					PrimaryKey: []string{"id"},
				})
				if err != nil {
					return nil, err
				}
				return &Ydb_Table.DescribeTableResponse{Operation: &Ydb_Operations.Operation{Ready: true, Status: Ydb.StatusIds_SUCCESS, Result: result}}, nil
			}}, queryServer{})
			table, err := client.DescribeTable(context.Background(), tc.name)
			if tc.wantError {
				assert.ErrorContains(t, err, "relative table paths must not contain '..' segments")
				select {
				case sent := <-requests:
					assert.Fail(t, "rejected relative path reached YDB", "path: %q", sent)
				default:
				}
				return
			}
			require.NoError(t, err)
			select {
			case sent := <-requests:
				require.Equal(t, tc.path, sent, "path = %q, want %q", sent, tc.path)
			default:
				require.FailNow(t, "table was not described")
			}
			require.Equal(t, tc.name, table.Name)
			require.NotEmpty(t, table.Columns)
			require.Equal(t, tc.name, table.Columns[0].Table)
		})
	}
}

func TestDescribeTableAbsolutePathAndErrors(t *testing.T) {
	cases := []struct {
		name string
		op   *Ydb_Operations.Operation
		want string
	}{
		{"missing table", &Ydb_Operations.Operation{Ready: true, Status: Ydb.StatusIds_SCHEME_ERROR, Issues: []*Ydb_Issue.IssueMessage{{Message: "table absent"}}}, "SCHEME_ERROR: table absent"},
		{"unfinished", &Ydb_Operations.Operation{}, "completed operation"},
		{"missing operation", nil, "completed operation"},
		{"missing metadata", &Ydb_Operations.Operation{Ready: true, Status: Ydb.StatusIds_SUCCESS}, "no table metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tableServer{describe: func(_ context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
				assert.Equal(t, "/other/items", request.GetPath(), "absolute path changed: %q", request.GetPath())
				return &Ydb_Table.DescribeTableResponse{Operation: tc.op}, nil
			}}, queryServer{})
			_, err := client.DescribeTable(context.Background(), "/other/items")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestValidateQueryOnlyExplainsOriginalSQL(t *testing.T) {
	const sql = "DECLARE $id AS Uint64;\nUPSERT INTO items (id) VALUES ($id);"
	client := testClient(t, tableServer{}, queryServer{explain: func(request *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		checkHeadersAndDeadline(t, stream.Context())
		assert.Equal(t, Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN, request.GetExecMode())
		assert.Equal(t, sql, request.GetQueryContent().GetText())
		assert.Equal(t, Ydb_Query.Syntax_SYNTAX_YQL_V1, request.GetQueryContent().GetSyntax())
		assert.Nil(t, request.GetTxControl())
		assert.Empty(t, request.GetSessionId())
		assert.Empty(t, request.GetParameters())
		return stream.Send(&Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS})
	}})
	require.NoError(t, client.ValidateQuery(context.Background(), sql))
}

func TestValidateQueryResponseErrors(t *testing.T) {
	cases := []struct {
		name  string
		parts []*Ydb_Query.ExecuteQueryResponsePart
		want  string
	}{
		{"empty", nil, "empty response stream"},
		{"late error", []*Ydb_Query.ExecuteQueryResponsePart{{Status: Ydb.StatusIds_SUCCESS}, {Status: Ydb.StatusIds_BAD_REQUEST, Issues: []*Ydb_Issue.IssueMessage{{Message: "compilation failed", Issues: []*Ydb_Issue.IssueMessage{{Message: "missing column"}}}}}}, "compilation failed; missing column"},
		{"unexpected result", []*Ydb_Query.ExecuteQueryResponsePart{{Status: Ydb.StatusIds_SUCCESS, ResultSet: &Ydb.ResultSet{}}}, "unexpected execution metadata"},
		{"unexpected transaction", []*Ydb_Query.ExecuteQueryResponsePart{{Status: Ydb.StatusIds_SUCCESS, TxMeta: &Ydb_Query.TransactionMeta{Id: "tx"}}}, "unexpected execution metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tableServer{}, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
				for _, part := range tc.parts {
					if err := stream.Send(part); err != nil {
						return err
					}
				}
				return nil
			}})
			err := client.ValidateQuery(context.Background(), "SELECT 1;")
			require.False(t, err == nil || !strings.Contains(err.Error(), tc.want), "got %v, want %q", err, tc.want)
		})
	}
}

func TestRequestDeadlineAndCancellation(t *testing.T) {
	client := testClient(t, tableServer{}, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		<-stream.Context().Done()
		return status.FromContextError(stream.Context().Err()).Err()
	}})
	client.timeout = 20 * time.Millisecond
	err := client.ValidateQuery(context.Background(), "SELECT 1;")
	require.Equal(t, codes.DeadlineExceeded, status.Code(errors.Unwrap(err)), "expected deadline, got %v", err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.ValidateQuery(ctx, "SELECT 1;")
	require.Equal(t, codes.Canceled, status.Code(errors.Unwrap(err)), "expected cancellation, got %v", err)
}

func checkHeadersAndDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	md, _ := metadata.FromIncomingContext(ctx)
	assert.False(t, strings.Join(md.Get("x-ydb-database"), ",") != "/local" || strings.Join(md.Get("x-ydb-auth-ticket"), ",") != "test-token", "wrong database/auth headers: %v", md)
	deadline, ok := ctx.Deadline()
	assert.True(t, ok && time.Until(deadline) <= time.Second, "missing configured per-RPC deadline")
}

func TestTLSConnectionWithCustomCA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sqlc-ydb test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})))
	queryservice.RegisterQueryServiceServer(server, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		return stream.Send(&Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS})
	}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	client, err := New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Secure: true, CAFile: ca, Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.ValidateQuery(context.Background(), "SELECT 1;"))
	untrusted, err := New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Secure: true, Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = untrusted.Close() })
	require.ErrorContains(t, untrusted.ValidateQuery(context.Background(), "SELECT 1;"), "certificate")
}

func TestInvalidCA(t *testing.T) {
	ca := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(ca, []byte("not a certificate"), 0o600))
	_, err := New(config.ResolvedDatabase{Secure: true, CAFile: ca, Timeout: time.Second})
	require.False(t, err == nil || !strings.Contains(err.Error(), "no valid PEM"), "got %v", err)
}

func TestValidateQuerySuggestsExplicitParameterDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		issue    *Ydb_Issue.IssueMessage
		wantHint bool
	}{
		{"unknown parameter", &Ydb_Issue.IssueMessage{Message: "Unknown name: $id"}, true},
		{"nested parameter", &Ydb_Issue.IssueMessage{Message: "Type annotation", Issues: []*Ydb_Issue.IssueMessage{{Message: "Unknown name: $имя"}}}, true},
		{"lowercase parameter", &Ydb_Issue.IssueMessage{Message: "unknown name: $id"}, true},
		{"uppercase nested parameter", &Ydb_Issue.IssueMessage{Message: "Type annotation", Issues: []*Ydb_Issue.IssueMessage{{Message: "UNKNOWN NAME: $Id"}}}, true},
		{"surrounding whitespace", &Ydb_Issue.IssueMessage{Message: " \n  Unknown name: $id \t"}, true},
		{"lowercase unknown column", &Ydb_Issue.IssueMessage{Message: "unknown name: title"}, false},
		{"lowercase nested unrelated syntax error", &Ydb_Issue.IssueMessage{Message: "Type annotation", Issues: []*Ydb_Issue.IssueMessage{{Message: "unexpected token: unknown name: $id"}}}, false},
		{"different diagnostic wording", &Ydb_Issue.IssueMessage{Message: "Unresolved identifier: $id"}, false},
		{"unknown column", &Ydb_Issue.IssueMessage{Message: "Unknown name: title"}, false},
		{"unrelated syntax error", &Ydb_Issue.IssueMessage{Message: "Unexpected token: Unknown name: $id"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, tableServer{}, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
				return stream.Send(&Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_GENERIC_ERROR, Issues: []*Ydb_Issue.IssueMessage{tc.issue}})
			}})
			err := client.ValidateQuery(context.Background(), "SELECT $id;")
			require.False(t, err == nil || !strings.Contains(err.Error(), "YDB GENERIC_ERROR") || !strings.Contains(err.Error(), tc.issue.GetMessage()), "server diagnostic was lost: %v", err)
			for _, nested := range tc.issue.GetIssues() {
				require.True(t, strings.Contains(err.Error(), nested.GetMessage()), "nested diagnostic was changed: %v", err)
			}
			require.Equal(t, tc.wantHint, strings.Contains(err.Error(), "DECLARE $var AS <YQL type>;"), "declaration hint mismatch: %v", err)
		})
	}
}
