-- name: ListVenues :many
DECLARE $city AS Utf8;
SELECT id, slug, name, city, status, statuses, spotify_playlist, songkick_id, tags, created_at
FROM venue
WHERE city = $city
ORDER BY name;

-- name: DeleteVenue :exec
DECLARE $slug AS Utf8;
DELETE FROM venue
WHERE slug = $slug AND slug = $slug;

-- name: GetVenue :one
DECLARE $slug AS Utf8;
DECLARE $city AS Utf8;
SELECT id, slug, name, city, status, statuses, spotify_playlist, songkick_id, tags, created_at
FROM venue
WHERE slug = $slug AND city = $city;

-- name: CreateVenue :one
DECLARE $id AS Uint64;
DECLARE $slug AS Utf8;
DECLARE $name AS Utf8;
DECLARE $city AS Utf8;
DECLARE $created_at AS Optional<Timestamp>;
DECLARE $spotify_playlist AS Utf8;
DECLARE $status AS Utf8;
DECLARE $statuses AS Optional<Json>;
DECLARE $tags AS Optional<Json>;
INSERT INTO venue (
    id,
    slug,
    name,
    city,
    created_at,
    spotify_playlist,
    status,
    statuses,
    tags
) VALUES (
    $id,
    $slug,
    $name,
    $city,
    $created_at,
    $spotify_playlist,
    $status,
    $statuses,
    $tags
) RETURNING id;

-- name: UpdateVenueName :one
DECLARE $name AS Utf8;
DECLARE $slug AS Utf8;
UPDATE venue
SET name = $name
WHERE slug = $slug
RETURNING id;

-- name: VenueCountByCity :many
SELECT
    city,
    COUNT(*) AS venue_count
FROM venue
GROUP BY city
ORDER BY city;
