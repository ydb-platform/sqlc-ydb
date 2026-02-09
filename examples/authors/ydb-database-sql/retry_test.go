package db

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueries_CreateAuthor_Retry(t *testing.T) {
	for _, tt := range []struct {
		name string
		db   DBTX
		err  error
	}{
		{
			name: "database/sql driver",
			db:   &sql.DB{},
			err:  nil,
		},
		{
			name: "database/sql conn",
			db:   &sql.Conn{},
			err:  nil,
		},
		{
			name: "database/sql tx",
			db:   &sql.Tx{},
			err:  nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.db).CreateAuthor(t.Context(), CreateAuthorParams{})
			if tt.err != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
