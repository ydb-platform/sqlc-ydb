package namespaces_test

import (
	"context"
	"testing"
	"time"

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
			if err := db.Driver.Scheme().RemoveDirectory(ctx, created[i]); err != nil {
				t.Errorf("remove owned directory %s: %v", created[i], err)
			}
		}
	})
	for _, directory := range []string{"/local/sqlc_namespaces", "/local/sqlc_namespaces/a", "/local/sqlc_namespaces/b"} {
		// MakeDirectory also succeeds for an existing directory; inspect first
		// so cleanup does not remove a directory owned by another caller.
		if _, err := db.Driver.Scheme().DescribePath(db.Context, directory); err == nil {
			continue
		} else if !ydb.IsOperationErrorSchemeError(err) && !ydb.IsOperationErrorNotFoundError(err) {
			t.Fatal(err)
		}
		if err := db.Driver.Scheme().MakeDirectory(db.Context, directory); err != nil {
			if !ydb.IsOperationErrorAlreadyExistsError(err) {
				t.Fatal(err)
			}
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
	if err := n.UpsertPrimaryUser(ctx, native.UpsertPrimaryUserParams{ID: 1, Name: "native primary"}); err != nil {
		t.Fatal(err)
	}
	if err := n.UpsertSecondaryUser(ctx, native.UpsertSecondaryUserParams{ID: 1, Name: "native secondary"}); err != nil {
		t.Fatal(err)
	}
	primary, err := s.GetPrimaryUser(ctx, 1)
	if err != nil || primary.Name != "native primary" {
		t.Fatalf("SQL primary: %v %v", primary, err)
	}
	secondary, err := s.GetSecondaryUser(ctx, 1)
	if err != nil || secondary.Name != "native secondary" {
		t.Fatalf("SQL secondary: %v %v", secondary, err)
	}
	if err := s.UpsertPrimaryUser(ctx, sq.UpsertPrimaryUserParams{ID: 2, Name: "SQL primary"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSecondaryUser(ctx, sq.UpsertSecondaryUserParams{ID: 2, Name: "SQL secondary"}); err != nil {
		t.Fatal(err)
	}
	nativePrimary, err := n.GetPrimaryUser(ctx, 2)
	if err != nil || nativePrimary.Name != "SQL primary" {
		t.Fatalf("native primary: %v %v", nativePrimary, err)
	}
	nativeSecondary, err := n.GetSecondaryUser(ctx, 2)
	if err != nil || nativeSecondary.Name != "SQL secondary" {
		t.Fatalf("native secondary: %v %v", nativeSecondary, err)
	}
	nativeIndex, err := n.FindPrimaryUsersByName(ctx, "SQL primary")
	if err != nil || len(nativeIndex) != 1 || nativeIndex[0].ID != 2 {
		t.Fatalf("native index: %v %v", nativeIndex, err)
	}
	sqlIndex, err := s.FindPrimaryUsersByName(ctx, "native primary")
	if err != nil || len(sqlIndex) != 1 || sqlIndex[0].ID != 1 {
		t.Fatalf("SQL index: %v %v", sqlIndex, err)
	}
	nativeJoin, err := n.CompareUserNames(ctx)
	if err != nil || len(nativeJoin) != 2 || nativeJoin[0].PrimaryName != "native primary" || nativeJoin[0].SecondaryName == nil || *nativeJoin[0].SecondaryName != "native secondary" {
		t.Fatalf("native absolute-path join: %v %v", nativeJoin, err)
	}
	sqlJoin, err := s.CompareUserNames(ctx)
	if err != nil || len(sqlJoin) != 2 || sqlJoin[1].PrimaryName != "SQL primary" || sqlJoin[1].SecondaryName == nil || *sqlJoin[1].SecondaryName != "SQL secondary" {
		t.Fatalf("SQL absolute-path join: %v %v", sqlJoin, err)
	}
}
