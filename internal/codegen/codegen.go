package codegen

import (
	"context"
	"fmt"

	"github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"
)

type Generator interface {
	Generate(ctx context.Context, req *pb.GenerateRequest) (*pb.GenerateResponse, error)

	Name() string
}

type API string

const (
	GoSDK       = API("ydb-go-sdk")
	DatabaseSql = API("database/sql")
	PythonSDK   = API("ydb-python-sdk")
)

func New(api API) Generator {
	switch api {
	case GoSDK:
		return generatorGoSdk{}
	case DatabaseSql:
		return generatorDatabaseSQL{}
	case PythonSDK:
		return generatorPythonSDK{}
	default:
		panic(fmt.Sprintf("unknown API %q", api))
	}
}
