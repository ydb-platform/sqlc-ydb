-- name: ListCities :many
SELECT slug, name
FROM city
ORDER BY name;

-- name: GetCity :one
DECLARE $slug AS Utf8;
SELECT slug, name
FROM city
WHERE slug = $slug;

-- name: CreateCity :one
DECLARE $name AS Utf8;
DECLARE $slug AS Utf8;
INSERT INTO city (
    name,
    slug
) VALUES (
    $name,
    $slug
) RETURNING slug, name;

-- name: UpdateCityName :exec
DECLARE $name AS Utf8;
DECLARE $slug AS Utf8;
UPDATE city
SET name = $name
WHERE slug = $slug;
