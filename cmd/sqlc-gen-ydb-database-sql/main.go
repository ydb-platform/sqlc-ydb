// sqlc-gen-ydb-database-sql is a process-based codegen plugin for sqlc (v2 config).
// It generates Go code using database/sql API (DBTX: ExecContext, QueryContext, QueryRowContext).
// Reads binary GenerateRequest from stdin, writes binary GenerateResponse to stdout.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/dbsql"
	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
	"google.golang.org/protobuf/proto"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "sqlc-gen-ydb-database-sql: %v\n", err)
		os.Exit(2)
	}
}

func run(ctx context.Context) error {
	reqBlob, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var req pb.GenerateRequest
	if err := proto.Unmarshal(reqBlob, &req); err != nil {
		return fmt.Errorf("unmarshal request: %w", err)
	}
	resp, err := dbsql.Generate(ctx, &req)
	if err != nil {
		return err
	}
	respBlob, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	w := bufio.NewWriter(os.Stdout)
	if _, err := w.Write(respBlob); err != nil {
		return err
	}
	return w.Flush()
}
