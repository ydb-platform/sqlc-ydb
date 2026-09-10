-- name: ListCities :many
SELECT slug, name
FROM city
ORDER BY name;

-- name: GetCity :one
SELECT slug, name
FROM city
WHERE slug = $slug;

-- name: CreateCity :one
INSERT INTO city (
    name,
    slug
) VALUES (
    $name,
    $slug
) RETURNING slug, name;

-- name: UpdateCityName :exec
UPDATE city
SET name = $name
WHERE slug = $slug;
