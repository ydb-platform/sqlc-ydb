-- name: ListVenues :many
SELECT id, slug, name, city, status, statuses, spotify_playlist, songkick_id, tags, created_at
FROM venue
WHERE city = $city
ORDER BY name;

-- name: DeleteVenue :exec
DELETE FROM venue
WHERE slug = $slug AND slug = $slug;

-- name: GetVenue :one
SELECT id, slug, name, city, status, statuses, spotify_playlist, songkick_id, tags, created_at
FROM venue
WHERE slug = $slug AND city = $city;

-- name: CreateVenue :one
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
