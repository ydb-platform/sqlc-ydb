package jets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	jetssql "example.com/sqlc-ydb-examples/jets/go/database/sql"
	jetsnative "example.com/sqlc-ydb-examples/jets/go/native"
)

func TestLive(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/jets/schema.sql",
		"DROP TABLE pilot_languages;",
		"DROP TABLE languages;",
		"DROP TABLE jets;",
		"DROP TABLE pilots;",
	)
	require.NoError(t, db.Native.Exec(db.Context, `UPSERT INTO pilots (id, name) VALUES
        (1, "Maverick"u),
        (2, "Iceman"u),
        (3, "Goose"u),
        (4, "Viper"u),
        (5, "Jester"u),
        (6, "Cougar"u),
        (7, "Merlin"u);`))

	native := jetsnative.New(db.Native)
	count, err := native.CountPilots(db.Context)
	require.NoError(t, err)
	require.Equal(t, uint64(7), count.PilotCount)

	portable := jetssql.New(db.SQL)
	pilots, err := portable.ListPilots(db.Context)
	require.NoError(t, err)
	require.Len(t, pilots, 5)
	for i, pilot := range pilots {
		require.Equal(t, int32(i+1), pilot.ID)
	}
	require.NoError(t, portable.DeletePilot(db.Context, 1))
	count, err = native.CountPilots(db.Context)
	require.NoError(t, err)
	require.Equal(t, uint64(6), count.PilotCount)
}
