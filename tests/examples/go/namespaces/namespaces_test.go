package namespaces_test

import (
	"context"
	"testing"

	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	sq "example.com/sqlc-ydb-examples/namespaces/go/database/sql"
	native "example.com/sqlc-ydb-examples/namespaces/go/native"
	"github.com/ydb-platform/ydb-go-sdk/v3"
)

func TestNamespaces(t *testing.T) {
	db := testdb.Open(t)
	var created []string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for i := len(created) - 1; i >= 0; i-- {
			assert.NoError(t, db.Driver.Scheme().RemoveDirectory(ctx, created[i]), "remove owned directory %s", created[i])
		}
	})
	for _, directory := range []string{"/local/sqlc_namespaces", "/local/sqlc_namespaces/a", "/local/sqlc_namespaces/b"} {
		// MakeDirectory also succeeds for an existing directory; inspect first
		// so cleanup does not remove a directory owned by another caller.
		if _, err := db.Driver.Scheme().DescribePath(db.Context, directory); err == nil {
			continue
		} else {
			require.True(t, ydb.IsOperationErrorSchemeError(err) || ydb.IsOperationErrorNotFoundError(err), "describe %s: %v", directory, err)
		}
		if err := db.Driver.Scheme().MakeDirectory(db.Context, directory); err != nil {
			require.True(t, ydb.IsOperationErrorAlreadyExistsError(err), "create %s: %v", directory, err)
		} else {
			created = append(created, directory)
		}
	}
	// Apply each source separately: TablePathPrefix is request-global on YDB.
	// Apply registers table cleanup only after CREATE succeeds, preserving any
	// pre-existing tables if this example database is already in use.
	db.Apply(t, "../../../../examples/namespaces/schema/a.sql", "DROP TABLE `/local/sqlc_namespaces/a/users`;")
	db.Apply(t, "../../../../examples/namespaces/schema/b.sql", "DROP TABLE `/local/sqlc_namespaces/b/users`;")
	ctx := db.Context
	n, s := native.New(db.Native), sq.New(db.SQL)
	require.NoError(t, n.UpsertPrimaryUser(ctx, native.UpsertPrimaryUserParams{ID: 1, Name: "native primary"}))
	require.NoError(t, n.UpsertSecondaryUser(ctx, native.UpsertSecondaryUserParams{ID: 1, Name: "native secondary"}))
	primary, err := s.GetPrimaryUser(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "native primary", primary.Name)
	secondary, err := s.GetSecondaryUser(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "native secondary", secondary.Name)
	require.NoError(t, s.UpsertPrimaryUser(ctx, sq.UpsertPrimaryUserParams{ID: 2, Name: "SQL primary"}))
	require.NoError(t, s.UpsertSecondaryUser(ctx, sq.UpsertSecondaryUserParams{ID: 2, Name: "SQL secondary"}))
	nativePrimary, err := n.GetPrimaryUser(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, "SQL primary", nativePrimary.Name)
	nativeSecondary, err := n.GetSecondaryUser(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, "SQL secondary", nativeSecondary.Name)
	nativeIndex, err := n.FindPrimaryUsersByName(ctx, "SQL primary")
	require.NoError(t, err)
	require.Len(t, nativeIndex, 1)
	require.Equal(t, uint64(2), nativeIndex[0].ID)
	sqlIndex, err := s.FindPrimaryUsersByName(ctx, "native primary")
	require.NoError(t, err)
	require.Len(t, sqlIndex, 1)
	require.Equal(t, uint64(1), sqlIndex[0].ID)
	nativeJoin, err := n.CompareUserNames(ctx)
	require.NoError(t, err)
	require.Len(t, nativeJoin, 2)
	require.Equal(t, "native primary", nativeJoin[0].PrimaryName)
	require.NotNil(t, nativeJoin[0].SecondaryName)
	require.Equal(t, "native secondary", *nativeJoin[0].SecondaryName)
	sqlJoin, err := s.CompareUserNames(ctx)
	require.NoError(t, err)
	require.Len(t, sqlJoin, 2)
	require.Equal(t, "SQL primary", sqlJoin[1].PrimaryName)
	require.NotNil(t, sqlJoin[1].SecondaryName)
	require.Equal(t, "SQL secondary", *sqlJoin[1].SecondaryName)
}
