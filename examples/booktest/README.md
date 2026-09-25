# Booktest

This example adapts sqlc's `booktest` examples at commit `3c2546a4b47fabbcec3e07df420effb1a464728f` to YDB and generates all 18 runtime profiles configured in [`sqlc.yaml`](sqlc.yaml).

The adaptation keeps the author and book CRUD queries, title/year lookup, author join, tag-overlap lookup, and greeting query covered by the upstream dialect variants. Create queries take explicit `Uint64` identifiers in place of upstream serial keys. The upstream enum is represented as `Utf8` (`FICTION` or `NONFICTION`), availability uses YDB `Timestamp`, and tags use YDB `Json`. The tag query converts each JSON array to a string list and uses a YQL set to test for overlap, following the PostgreSQL variant rather than the serialized string equality in MySQL/SQLite. `SayHello` uses YQL string concatenation with a required `Utf8` input in place of the PostgreSQL user-defined function.

`RemoveBookTag` filters one book's JSON string array and writes the result in one `UPDATE`. The lambda captures the declared tag parameter; removing the last matching tag writes an empty JSON array.

Foreign-key, unique-index, default-value, and auto-increment behavior from the upstream schemas is not declared here because the supported YDB schema subset does not provide those contracts. Callers must validate `book_type` values and supply identifiers, timestamps, and tags explicitly.

`ListAuthorsWithRecentBooks` uses a scalar `IN (SELECT ...)` to find authors with a book published since a given year. `ListBooksWithRecentEditions` matches the `(author_id, book_type)` pair to find all books in categories for which that author has a recent edition. YQL requires one tuple-valued projection, `SELECT (author_id, book_type)`, rather than two separately projected columns. `DeleteBooksByAuthorName` demonstrates the same scalar membership predicate in a write query.

The tuple example explicitly declares the year parameter. For jOOQ this uses its existing typed JDBC execution path, which preserves the YQL tuple projection and applies configured table mappings. Undeclared scalar membership queries use the typed jOOQ DSL; undeclared tuple membership queries report an actionable target limitation because the pinned DSL renders a different row-constructor syntax. Each subquery has its own table aliases and does not reference the outer query.

`DeleteAuthorWithBooks` deletes an author's books and then the author in one `:exec` script with a shared `Uint64` parameter. Each generated method submits the complete script once; the caller retains connection and transaction ownership. The explicit `DECLARE` selects jOOQ's typed JDBC path, which also preserves both operations in one execution.

`UpdateAuthorAndListBooks` updates an author and returns their books through `:many`. `SelectAuthorAndDeleteBooks` returns the author through `:one` and then deletes their books. Both methods execute the complete script, including operations after the SELECT, before returning. An empty `:one` result does not roll back successful writes; use a caller-owned transaction when that policy is needed. Their explicit declarations keep jOOQ on the same whole-script JDBC path.

`ListAuthorBookTitles` reuses named selections, groups a bounded list of book titles, and joins it with a derived author selection. The resulting title list is serialized to JSON for runtimes that do not expose YQL collection values directly; authors without qualifying books are excluded by the inner join.

`InspectBookText` is a read-only scalar example of documented String, Unicode, Url, Math, Yson, and Pire UDFs. It demonstrates nested resource-producing calls and a callable regular-expression matcher, with an optional host value and one shared text parameter.
