// Package database reads YDB schema metadata and compiles queries without executing them.
package database

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	queryservice "github.com/ydb-platform/ydb-go-genproto/Ydb_Query_V1"
	tableservice "github.com/ydb-platform/ydb-go-genproto/Ydb_Table_V1"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Issue"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Operations"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Query"
	"github.com/ydb-platform/ydb-go-genproto/protos/Ydb_Table"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/ydb-platform/sqlc-ydb/internal/config"
	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

// Client owns one gRPC connection. Both supported RPCs are sessionless.
type Client struct {
	conn     *grpc.ClientConn
	tables   tableservice.TableServiceClient
	queries  queryservice.QueryServiceClient
	database string
	token    string
	timeout  time.Duration
}

func New(settings config.ResolvedDatabase) (*Client, error) {
	if settings.Timeout <= 0 {
		return nil, errors.New("database timeout must be positive")
	}
	transport := insecure.NewCredentials()
	if settings.Secure {
		configuration := &tls.Config{MinVersion: tls.VersionTLS12}
		if settings.CAFile != "" {
			pem, err := os.ReadFile(settings.CAFile)
			if err != nil {
				return nil, fmt.Errorf("read database.ca_file: %w", err)
			}
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, fmt.Errorf("load system certificate authorities: %w", err)
			}
			if !roots.AppendCertsFromPEM(pem) {
				return nil, errors.New("database.ca_file contains no valid PEM certificates")
			}
			configuration.RootCAs = roots
		}
		transport = credentials.NewTLS(configuration)
	}
	conn, err := grpc.NewClient(settings.Endpoint, grpc.WithTransportCredentials(transport))
	if err != nil {
		return nil, fmt.Errorf("create database connection: %w", err)
	}
	return &Client{
		conn: conn, tables: tableservice.NewTableServiceClient(conn), queries: queryservice.NewQueryServiceClient(conn),
		database: settings.Database, token: settings.AuthToken, timeout: settings.Timeout,
	}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("x-ydb-database", c.database)
	if c.token != "" {
		md.Set("x-ydb-auth-ticket", c.token)
	}
	return context.WithTimeout(metadata.NewOutgoingContext(ctx, md), c.timeout)
}

// DescribeTable preserves the source table name; only the RPC path is made absolute.
func (c *Client) DescribeTable(ctx context.Context, name string) (model.Table, error) {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	tablePath := name
	if !path.IsAbs(tablePath) {
		for _, segment := range strings.Split(tablePath, "/") {
			if segment == ".." {
				return model.Table{}, fmt.Errorf("describe table %q: relative table paths must not contain '..' segments; use a direct relative path or an explicit absolute path", name)
			}
		}
		tablePath = path.Join(c.database, tablePath)
	}
	response, err := c.tables.DescribeTable(ctx, &Ydb_Table.DescribeTableRequest{
		Path:            tablePath,
		OperationParams: &Ydb_Operations.OperationParams{OperationMode: Ydb_Operations.OperationParams_SYNC},
	})
	if err != nil {
		return model.Table{}, fmt.Errorf("describe table %q: %w", name, err)
	}
	operation := response.GetOperation()
	if operation == nil || !operation.GetReady() {
		return model.Table{}, fmt.Errorf("describe table %q: server did not return a completed operation", name)
	}
	if err := statusError(operation.GetStatus(), operation.GetIssues()); err != nil {
		return model.Table{}, fmt.Errorf("describe table %q: %w", name, err)
	}
	if operation.GetResult() == nil {
		return model.Table{}, fmt.Errorf("describe table %q: server returned no table metadata", name)
	}
	var description Ydb_Table.DescribeTableResult
	if err := operation.GetResult().UnmarshalTo(&description); err != nil {
		return model.Table{}, fmt.Errorf("describe table %q: decode table metadata: %w", name, err)
	}
	table, err := decodeTable(name, &description)
	if err != nil {
		return model.Table{}, fmt.Errorf("describe table %q: %w", name, err)
	}
	return table, nil
}

// ValidateQuery compiles the full query with EXPLAIN without requiring parameter values.
func (c *Client) ValidateQuery(ctx context.Context, sql string) error {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	stream, err := c.queries.ExecuteQuery(ctx, &Ydb_Query.ExecuteQueryRequest{
		ExecMode: Ydb_Query.ExecMode_EXEC_MODE_EXPLAIN,
		Query: &Ydb_Query.ExecuteQueryRequest_QueryContent{QueryContent: &Ydb_Query.QueryContent{
			Syntax: Ydb_Query.Syntax_SYNTAX_YQL_V1, Text: sql,
		}},
	})
	if err != nil {
		return fmt.Errorf("explain query: %w", err)
	}
	received := false
	for {
		part, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if !received {
				return errors.New("explain query: server returned an empty response stream")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("explain query: %w", err)
		}
		received = true
		if err := statusError(part.GetStatus(), part.GetIssues()); err != nil {
			if hasUndeclaredParameter(part.GetIssues()) {
				err = fmt.Errorf("%w\nhint: declare query parameter types explicitly with DECLARE $var AS <YQL type>;", err)
			}
			return fmt.Errorf("explain query: %w", err)
		}
		if part.GetResultSet() != nil || part.GetTxMeta() != nil {
			return errors.New("explain query: unexpected execution metadata in compile-only response")
		}
	}
}

func statusError(status Ydb.StatusIds_StatusCode, issues []*Ydb_Issue.IssueMessage) error {
	if status == Ydb.StatusIds_SUCCESS {
		return nil
	}
	messages := make([]string, 0, len(issues))
	var appendIssues func([]*Ydb_Issue.IssueMessage)
	appendIssues = func(issues []*Ydb_Issue.IssueMessage) {
		for _, issue := range issues {
			if issue.GetMessage() != "" {
				message := issue.GetMessage()
				if position := issue.GetPosition(); position != nil {
					message = fmt.Sprintf("%d:%d: %s", position.GetRow(), position.GetColumn(), message)
				}
				messages = append(messages, message)
			}
			appendIssues(issue.GetIssues())
		}
	}
	appendIssues(issues)
	if len(messages) == 0 {
		return fmt.Errorf("YDB %s", status)
	}
	return fmt.Errorf("YDB %s: %s", status, strings.Join(messages, "; "))
}

func hasUndeclaredParameter(issues []*Ydb_Issue.IssueMessage) bool {
	for _, issue := range issues {
		// YDB reports undeclared parameters as unknown names, including in
		// nested type-annotation issues. Do not reinterpret other errors.
		message := strings.ToLower(strings.TrimSpace(issue.GetMessage()))
		if strings.HasPrefix(message, "unknown name: $") || hasUndeclaredParameter(issue.GetIssues()) {
			return true
		}
	}
	return false
}
