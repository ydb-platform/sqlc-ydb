-- name: ListCities :many
SELECT * FROM city
ORDER BY name;

-- name: GetCity :one
SELECT * FROM city
WHERE slug = $slug LIMIT 1;

-- name: CreateCity :one
INSERT INTO city (name, slug)
VALUES ($name, $slug)
RETURNING *;

-- name: UpdateCityName :exec
UPDATE city
SET name = $name
WHERE slug = $slug;

-- name: ListVenues :many
SELECT * FROM venue
WHERE city = $city
ORDER BY name;

-- name: DeleteVenue :exec
DELETE FROM venue
WHERE slug = $slug;

-- name: GetVenue :one
SELECT * FROM venue
WHERE slug = $slug AND city = $city LIMIT 1;

-- name: CreateVenue :one
INSERT INTO venue (
    id,
    slug,
    name,
    city,
    created_at,
    spotify_playlist,
    status,
    tags
) VALUES (
    $id,
    $slug,
    $name,
    $city,
    $created_at,
    $spotify_playlist,
    $status,
    $tags
)
RETURNING id;

-- name: UpdateVenueName :one
UPDATE venue
SET name = $name
WHERE slug = $slug
RETURNING id;

-- name: VenueCountByCity :many
SELECT city, COUNT(*) AS count
FROM venue
GROUP BY city
ORDER BY city;
