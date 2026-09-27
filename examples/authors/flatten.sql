-- name: ListAuthorNameWords :many
SELECT id, word
FROM (SELECT id, Unicode::SplitToList(name, " "u) AS words FROM authors)
FLATTEN LIST BY words AS word
ORDER BY id, word;
