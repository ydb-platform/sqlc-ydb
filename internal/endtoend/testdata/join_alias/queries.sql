-- name: ListAuthorBooks :many
SELECT a.id AS author_id, a.name AS author_name, b.title AS book_title
FROM authors AS a JOIN books AS b ON a.id = b.author_id;
