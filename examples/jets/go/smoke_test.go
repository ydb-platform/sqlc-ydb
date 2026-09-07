package jets_test

import (
	"testing"

	"example.com/sqlc-ydb-examples/internal/testdb"
	jetssql "example.com/sqlc-ydb-examples/jets/go/database/sql"
	jetsnative "example.com/sqlc-ydb-examples/jets/go/native"
)

func TestLive(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../schema.sql",
		"DROP TABLE pilot_languages;",
		"DROP TABLE languages;",
		"DROP TABLE jets;",
		"DROP TABLE pilots;",
	)
	if err := db.Native.Exec(db.Context, `UPSERT INTO pilots (id, name) VALUES
        (1, "Maverick"u),
        (2, "Iceman"u),
        (3, "Goose"u),
        (4, "Viper"u),
        (5, "Jester"u),
        (6, "Cougar"u),
        (7, "Merlin"u);`); err != nil {
		t.Fatal(err)
	}

	native := jetsnative.New(db.Native)
	count, err := native.CountPilots(db.Context)
	if err != nil || count.PilotCount != 7 {
		t.Fatalf("native count = %#v, %v", count, err)
	}

	portable := jetssql.New(db.SQL)
	pilots, err := portable.ListPilots(db.Context)
	if err != nil || len(pilots) != 5 {
		t.Fatalf("database/sql pilots = %#v, %v", pilots, err)
	}
	for i, pilot := range pilots {
		if pilot.ID != int32(i+1) {
			t.Fatalf("pilot order/limit = %#v", pilots)
		}
	}
	if err := portable.DeletePilot(db.Context, 1); err != nil {
		t.Fatal(err)
	}
	count, err = native.CountPilots(db.Context)
	if err != nil || count.PilotCount != 6 {
		t.Fatalf("count after delete = %#v, %v", count, err)
	}
}
