# Booktest

This example adapts sqlc's `booktest` examples at commit
`3c2546a4b47fabbcec3e07df420effb1a464728f` to YDB and generates both native
YDB SDK and `database/sql` Go packages.

The adaptation keeps the author and book CRUD queries, title/year lookup,
author join, tag-overlap lookup, and greeting query covered by the upstream
dialect variants. Create queries take explicit `Uint64` identifiers in place of
upstream serial keys. The upstream enum is represented as `Utf8`
(`FICTION` or `NONFICTION`), availability uses YDB `Timestamp`, and tags use YDB `Json`.
The tag query converts each JSON array to a string list and uses a YQL set to
test for overlap, following the PostgreSQL variant rather than the serialized
string equality in MySQL/SQLite. `SayHello` uses YQL string concatenation with a
required `Utf8` input in place of the PostgreSQL user-defined function.

Foreign-key, unique-index, default-value, and auto-increment behavior from the
upstream schemas is not declared here because the supported YDB schema subset
does not provide those contracts. Callers must validate `book_type` values and
supply identifiers, timestamps, and tags explicitly.
