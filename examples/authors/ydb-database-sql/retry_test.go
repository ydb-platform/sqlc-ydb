package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestQueries_CreateAuthor_Retry(t *testing.T) {
	args := CreateAuthorParams{
		Name: "John Doe",
		Bio: func(bio string) *string {
			return &bio
		}("John Doe bio"),
	}
	for _, tt := range []struct {
		name string
		db   func(ctx context.Context) (DBTX, error)
		err  error
	}{
		{
			name: "database/sql driver",
			db: func(ctx context.Context) (DBTX, error) {
				db, mock, err := sqlmock.New()
				if err != nil {
					return nil, err
				}
				mock.ExpectQuery(createAuthor).
					WithArgs(
						sql.Named("name", args.Name),
						sql.Named("bio", args.Bio),
					).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "bio"}).
						AddRow(1, args.Name, args.Bio))

				return db, nil
			},
			err: nil,
		},
		{
			name: "database/sql conn",
			db: func(ctx context.Context) (DBTX, error) {
				db, mock, err := sqlmock.New()
				if err != nil {
					return nil, err
				}
				mock.ExpectQuery(`^SELECT name, email FROM users WHERE id = \?$`).
					WithArgs(1).
					WillReturnRows(sqlmock.NewRows([]string{"name", "email"}).
						AddRow(args.Name, "John Doe bio"))
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
				db, mock, err := sqlmock.New()
				if err != nil {
					return nil, err
				}
				mock.ExpectBegin()
				mock.ExpectQuery(`^SELECT name, email FROM users WHERE id = \?$`).
					WithArgs(1).
					WillReturnRows(sqlmock.NewRows([]string{"name", "email"}).
						AddRow("John Doe", "john@example.com"))
				mock.ExpectCommit()
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
			dbtx, err := tt.db(t.Context())
			require.NoError(t, err)
			_, err = New(dbtx).CreateAuthor(t.Context(), args)
			if tt.err != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
