-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT `id`, `name`, `bio` FROM `authors` WHERE `id` = $author_id;
