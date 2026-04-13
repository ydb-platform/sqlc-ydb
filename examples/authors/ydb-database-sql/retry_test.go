package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestQueries_CreateAuthor_Retry(t *testing.T) {
	id := uint64(1)
	name := "John Doe"
	bio := "John Doe bio"
	bioPtr := &bio

	// SQL uses $id/$name/$bio — sqlmock's default matcher treats $ as regexp; use equality.
	for _, tt := range []struct {
		name string
		db   func(ctx context.Context) (DBTX, error)
		err  error
	}{
		{
			name: "database/sql driver",
			db: func(ctx context.Context) (DBTX, error) {
				db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
				if err != nil {
					return nil, err
				}
				mock.ExpectQuery(createAuthor).
					WithArgs(
						sql.Named("id", id),
						sql.Named("name", name),
						sql.Named("bio", bioPtr),
					).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "bio"}).
						AddRow(id, name, bioPtr))

				return db, nil
			},
			err: nil,
		},
		{
			name: "database/sql conn",
			db: func(ctx context.Context) (DBTX, error) {
				db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
				if err != nil {
					return nil, err
				}
				mock.ExpectQuery(createAuthor).
					WithArgs(
						sql.Named("id", id),
						sql.Named("name", name),
						sql.Named("bio", bioPtr),
					).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "bio"}).
						AddRow(id, name, bioPtr))
				cc, err := db.Conn(ctx)
				if err != nil {
					return nil, err
				}

				return cc, nil
			},
			err: nil,
		},
		{
			name: "database/sql tx",
			db: func(ctx context.Context) (DBTX, error) {
				db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
				if err != nil {
					return nil, err
				}
				mock.ExpectBegin()
				mock.ExpectQuery(createAuthor).
					WithArgs(
						sql.Named("id", id),
						sql.Named("name", name),
						sql.Named("bio", bioPtr),
					).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "bio"}).
						AddRow(id, name, bioPtr))
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					return nil, err
				}

				return tx, nil
			},
			err: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			dbtx, err := tt.db(ctx)
			require.NoError(t, err)
			_, err = New(dbtx).CreateAuthor(ctx, id, name, bioPtr)
			if tt.err != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
