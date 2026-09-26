package ondeck_test

import (
	"context"
	"testing"

	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		assert.NoError(t, db.Native.Exec(ctx, "DROP TABLE "+venueTable+";"), "cleanup venue table")
	})
	require.NoError(t, db.Native.Exec(db.Context, `UPSERT INTO venues
        (id, status, slug, name, city, spotify_playlist)
        VALUES (1ul, "op!en"u, "legacy"u, "Legacy"u, "old-city"u, "spotify:legacy"u);`))
	db.Apply(t, "../../../../examples/ondeck/schema/0003_rename_venue.sql")
	venueTable = "venue"
	db.Apply(t, "../../../../examples/ondeck/schema/0004_add_created_at.sql")
	db.Apply(t, "../../../../examples/ondeck/schema/0005_drop_column.sql")
	db.Apply(t, "../../../../examples/ondeck/schema/0006_drop_playlist_not_null.sql")

	native := ondecknative.New(db.Native)
	legacy, err := native.GetVenue(db.Context, ondecknative.GetVenueParams{Slug: "legacy", City: "old-city"})
	require.NoError(t, err)
	require.Nil(t, legacy.CreatedAt)
	require.Nil(t, legacy.SongkickID)
	require.NotNil(t, legacy.SpotifyPlaylist)
	require.Equal(t, "spotify:legacy", *legacy.SpotifyPlaylist)
	require.NoError(t, native.DeleteVenue(db.Context, "legacy"))
	createdCity, err := native.CreateCity(db.Context, ondecknative.CreateCityParams{Name: "New York", Slug: "nyc"})
	require.NoError(t, err)
	require.Equal(t, "nyc", createdCity.Slug)

	portable := ondecksql.New(db.SQL)
	require.NoError(t, portable.UpdateCityName(db.Context, ondecksql.UpdateCityNameParams{Name: "New York City", Slug: "nyc"}))
	city, err := portable.GetCity(db.Context, "nyc")
	require.NoError(t, err)
	require.Equal(t, "New York City", city.Name)
	cities, err := native.ListCities(db.Context)
	require.NoError(t, err)
	require.Len(t, cities, 1)

	now := time.Now().UTC().Truncate(time.Microsecond)
	statuses := `["op!en","clo@sed"]`
	tags := `["jazz","live"]`
	createdVenue, err := native.CreateVenue(db.Context, ondecknative.CreateVenueParams{
		ID: 7, Slug: "blue-note", Name: "Blue Note", City: "nyc",
		CreatedAt: &now, Status: "op!en",
		Statuses: &statuses, Tags: &tags,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(7), createdVenue.ID)
	venue, err := portable.GetVenue(db.Context, ondecksql.GetVenueParams{Slug: "blue-note", City: "nyc"})
	require.NoError(t, err)
	require.NotNil(t, venue.CreatedAt)
	require.True(t, venue.CreatedAt.Equal(now))
	require.Nil(t, venue.SongkickID)
	require.Nil(t, venue.SpotifyPlaylist)
	require.Equal(t, &statuses, venue.Statuses)
	require.Equal(t, &tags, venue.Tags)
	venues, err := native.ListVenues(db.Context, "nyc")
	require.NoError(t, err)
	require.Len(t, venues, 1)
	require.Nil(t, venues[0].SpotifyPlaylist)
	counts, err := portable.VenueCountByCity(db.Context)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	require.Equal(t, uint64(1), counts[0].VenueCount)
	grouped, err := native.VenueCountByCityStatus(db.Context, 1)
	require.NoError(t, err)
	require.Len(t, grouped, 1)
	require.Equal(t, "nyc/op!en", grouped[0].CityStatus)
	require.Equal(t, uint64(1), grouped[0].VenueCount)
	filtered, err := portable.VenueCountByCityStatus(db.Context, 2)
	require.NoError(t, err)
	require.Empty(t, filtered)
	updated, err := native.UpdateVenueName(db.Context, ondecknative.UpdateVenueNameParams{Name: "Blue Note Jazz Club", Slug: "blue-note"})
	require.NoError(t, err)
	require.Equal(t, uint64(7), updated.ID)
	venue, err = portable.GetVenue(db.Context, ondecksql.GetVenueParams{Slug: "blue-note", City: "nyc"})
	require.NoError(t, err)
	require.Equal(t, "Blue Note Jazz Club", venue.Name)
	require.NoError(t, portable.DeleteVenue(db.Context, "blue-note"))
	_, err = native.GetVenue(db.Context, ondecknative.GetVenueParams{Slug: "blue-note", City: "nyc"})
	require.Error(t, err, "deleted venue is still readable")
	playlist := "spotify:new"
	_, err = native.CreateVenue(db.Context, ondecknative.CreateVenueParams{
		ID: 8, Slug: "with-playlist", Name: "With Playlist", City: "nyc", Status: "op!en", SpotifyPlaylist: &playlist,
	})
	require.NoError(t, err)
	populated, err := native.GetVenue(db.Context, ondecknative.GetVenueParams{Slug: "with-playlist", City: "nyc"})
	require.NoError(t, err)
	require.Equal(t, &playlist, populated.SpotifyPlaylist)
}
