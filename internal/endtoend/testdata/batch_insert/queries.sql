-- name: CreateBooks :exec
DECLARE $books AS List<Struct<book_id: Uint64, title: Optional<Utf8>, tags: Json>>;
INSERT INTO books (book_id, title, tags)
SELECT book_id, title, tags FROM AS_TABLE($books);
