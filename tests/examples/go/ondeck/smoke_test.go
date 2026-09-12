package ondeck_test

import (
	"context"
	"testing"
	"time"

	"example.com/sqlc-ydb-example-tests/internal/testdb"
	ondecksql "example.com/sqlc-ydb-examples/ondeck/go/database/sql"
	ondecknative "example.com/sqlc-ydb-examples/ondeck/go/native"
)

func TestLive(t *testing.T) {
	db := testdb.Open(t)
	db.Apply(t, "../../../../examples/ondeck/schema/0001_city.sql", "DROP TABLE city;")

	venueTable := "venues"
	db.Apply(t, "../../../../examples/ondeck/schema/0002_venue.sql")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Native.Exec(ctx, "DROP TABLE "+venueTable+";"); err != nil {
			t.Errorf("cleanup venue table: %v", err)
		}
	})
	if err := db.Native.Exec(db.Context, `UPSERT INTO venues
        (id, status, slug, name, city, spotify_playlist)
        VALUES (1ul, "op!en"u, "legacy"u, "Legacy"u, "old-city"u, "spotify:legacy"u);`); err != nil {
		t.Fatal(err)
	}
	db.Apply(t, "../../../../examples/ondeck/schema/0003_rename_venue.sql")
	venueTable = "venue"
	db.Apply(t, "../../../../examples/ondeck/schema/0004_add_created_at.sql")
	db.Apply(t, "../../../../examples/ondeck/schema/0005_drop_column.sql")

	native := ondecknative.New(db.Native)
	legacy, err := native.GetVenue(db.Context, ondecknative.GetVenueParams{Slug: "legacy", City: "old-city"})
	if err != nil || legacy.CreatedAt != nil || legacy.SongkickID != nil {
		t.Fatalf("migrated venue = %#v, %v", legacy, err)
	}
	if err := native.DeleteVenue(db.Context, "legacy"); err != nil {
		t.Fatal(err)
	}
	createdCity, err := native.CreateCity(db.Context, ondecknative.CreateCityParams{Name: "New York", Slug: "nyc"})
	if err != nil || createdCity.Slug != "nyc" {
		t.Fatalf("create city = %#v, %v", createdCity, err)
	}

	portable := ondecksql.New(db.SQL)
	if err := portable.UpdateCityName(db.Context, ondecksql.UpdateCityNameParams{Name: "New York City", Slug: "nyc"}); err != nil {
		t.Fatal(err)
	}
	city, err := portable.GetCity(db.Context, "nyc")
	if err != nil || city.Name != "New York City" {
		t.Fatalf("get city = %#v, %v", city, err)
	}
	cities, err := native.ListCities(db.Context)
	if err != nil || len(cities) != 1 {
		t.Fatalf("list cities = %#v, %v", cities, err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	statuses := `["op!en","clo@sed"]`
	tags := `["jazz","live"]`
	createdVenue, err := native.CreateVenue(db.Context, ondecknative.CreateVenueParams{
		ID: 7, Slug: "blue-note", Name: "Blue Note", City: "nyc",
		CreatedAt: &now, SpotifyPlaylist: "spotify:playlist:example", Status: "op!en",
		Statuses: &statuses, Tags: &tags,
	})
	if err != nil || createdVenue.ID != 7 {
		t.Fatalf("create venue = %#v, %v", createdVenue, err)
	}
	venue, err := portable.GetVenue(db.Context, ondecksql.GetVenueParams{Slug: "blue-note", City: "nyc"})
	if err != nil || venue.CreatedAt == nil || !venue.CreatedAt.Equal(now) || venue.SongkickID != nil || venue.Statuses == nil || *venue.Statuses != statuses || venue.Tags == nil || *venue.Tags != tags {
		t.Fatalf("get venue = %#v, %v", venue, err)
	}
	venues, err := native.ListVenues(db.Context, "nyc")
	if err != nil || len(venues) != 1 {
		t.Fatalf("list venues = %#v, %v", venues, err)
	}
	counts, err := portable.VenueCountByCity(db.Context)
	if err != nil || len(counts) != 1 || counts[0].VenueCount != 1 {
		t.Fatalf("venue counts = %#v, %v", counts, err)
	}
	updated, err := native.UpdateVenueName(db.Context, ondecknative.UpdateVenueNameParams{Name: "Blue Note Jazz Club", Slug: "blue-note"})
	if err != nil || updated.ID != 7 {
		t.Fatalf("update venue = %#v, %v", updated, err)
	}
	venue, err = portable.GetVenue(db.Context, ondecksql.GetVenueParams{Slug: "blue-note", City: "nyc"})
	if err != nil || venue.Name != "Blue Note Jazz Club" {
		t.Fatalf("venue after update = %#v, %v", venue, err)
	}
	if err := portable.DeleteVenue(db.Context, "blue-note"); err != nil {
		t.Fatal(err)
	}
	if _, err := native.GetVenue(db.Context, ondecknative.GetVenueParams{Slug: "blue-note", City: "nyc"}); err == nil {
		t.Fatal("deleted venue is still readable")
	}
}
