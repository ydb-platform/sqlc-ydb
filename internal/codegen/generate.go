package codegen

import (
	"context"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
)

// Generate produces code for the given destination (database/sql, ydb-go-sdk, or ydb-python-sdk).
func Generate(ctx context.Context, req *pb.GenerateRequest, dest Destination) (*pb.GenerateResponse, error) {
	switch dest {
	case DatabaseSQL:
		return generateDatabaseSQL(ctx, req)
	case YdbGoSDK:
		return generateYdbGoSDK(ctx, req)
	case YdbPythonSDK:
		return generateYdbPythonSDK(ctx, req)
	default:
		return generateYdbGoSDK(ctx, req) // fallback
	}
}
