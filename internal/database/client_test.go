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
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Client{conn: conn, tables: tableservice.NewTableServiceClient(conn), queries: queryservice.NewQueryServiceClient(conn), database: "/local", token: "test-token", timeout: time.Second}
}

func TestDescribeTableSessionlessMetadata(t *testing.T) {
	client := testClient(t, tableServer{describe: func(ctx context.Context, request *Ydb_Table.DescribeTableRequest) (*Ydb_Table.DescribeTableResponse, error) {
		checkHeadersAndDeadline(t, ctx)
		if request.GetPath() != "/local/items" || request.GetSessionId() != "" || request.GetOperationParams().GetOperationMode() != Ydb_Operations.OperationParams_SYNC {
			t.Errorf("unexpected describe request: %v", request)
		}
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
	if err != nil {
		t.Fatal(err)
	}
	if table.Name != "items" || len(table.Columns) != 2 || table.Columns[0].Table != "items" || !table.Columns[0].SequenceGenerated || table.Columns[1].Type.String() != "Optional<Utf8>" || len(table.PrimaryKey) != 1 || table.PrimaryKey[0] != "id" {
		t.Fatalf("wrong table metadata: %+v", table)
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
				if request.GetPath() != "/other/items" {
					t.Errorf("absolute path changed: %q", request.GetPath())
				}
				return &Ydb_Table.DescribeTableResponse{Operation: tc.op}, nil
			}}, queryServer{})
			_, err := client.DescribeTable(context.Background(), "/other/items")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateQueryOnlyExplainsOriginalSQL(t *testing.T) {
	const sql = "DECLARE $id AS Uint64;\nUPSERT INTO items (id) VALUES ($id);"
	client := testClient(t, tableServer{}, queryServer{explain: func(request *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		checkHeadersAndDeadline(t, stream.Context())
		if request.GetExecMode() != Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN || request.GetQueryContent().GetText() != sql || request.GetQueryContent().GetSyntax() != Ydb_Query.Syntax_SYNTAX_YQL_V1 || request.GetTxControl() != nil || request.GetSessionId() != "" || len(request.GetParameters()) != 0 {
			t.Errorf("request must only explain original SQL without parameter values: %v", request)
		}
		return stream.Send(&Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS})
	}})
	if err := client.ValidateQuery(context.Background(), sql); err != nil {
		t.Fatal(err)
	}
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
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRequestDeadlineAndCancellation(t *testing.T) {
	client := testClient(t, tableServer{}, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		<-stream.Context().Done()
		return status.FromContextError(stream.Context().Err()).Err()
	}})
	client.timeout = 20 * time.Millisecond
	if err := client.ValidateQuery(context.Background(), "SELECT 1;"); status.Code(errors.Unwrap(err)) != codes.DeadlineExceeded {
		t.Fatalf("expected deadline, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.ValidateQuery(ctx, "SELECT 1;"); status.Code(errors.Unwrap(err)) != codes.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func checkHeadersAndDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	md, _ := metadata.FromIncomingContext(ctx)
	if strings.Join(md.Get("x-ydb-database"), ",") != "/local" || strings.Join(md.Get("x-ydb-auth-ticket"), ",") != "test-token" {
		t.Errorf("wrong database/auth headers: %v", md)
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
		t.Error("missing configured per-RPC deadline")
	}
}

func TestTLSConnectionWithCustomCA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sqlc-ydb test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})))
	queryservice.RegisterQueryServiceServer(server, queryServer{explain: func(_ *Ydb_Query.ExecuteQueryRequest, stream queryservice.QueryService_ExecuteQueryServer) error {
		return stream.Send(&Ydb_Query.ExecuteQueryResponsePart{Status: Ydb.StatusIds_SUCCESS})
	}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Secure: true, CAFile: ca, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.ValidateQuery(context.Background(), "SELECT 1;"); err != nil {
		t.Fatal(err)
	}
	untrusted, err := New(config.ResolvedDatabase{Endpoint: listener.Addr().String(), Database: "/local", Secure: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = untrusted.Close() })
	if err := untrusted.ValidateQuery(context.Background(), "SELECT 1;"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected untrusted certificate error, got %v", err)
	}
}

func TestInvalidCA(t *testing.T) {
	ca := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(ca, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(config.ResolvedDatabase{Secure: true, CAFile: ca, Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "no valid PEM") {
		t.Fatalf("got %v", err)
	}
}
