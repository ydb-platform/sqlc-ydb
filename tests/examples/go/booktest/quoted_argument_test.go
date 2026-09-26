package booktest_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
)

func TestQuotedArgumentWireName(t *testing.T) {
	db := testdb.Open(t)
	require.NoError(t, db.Native.Exec(db.Context, "DECLARE $`display-name` AS Utf8; SELECT $`display-name` AS value;", query.WithParameters(ydb.ParamsBuilder().Param("$display-name").Text("value").Build())))
}
