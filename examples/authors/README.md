# Authors

The introductory sqlc workflow: create an author, fetch one row, list rows ordered by name, page through rows ordered by ID, and delete a row. `CreateAuthor` returns the inserted row. `UpsertAuthor` and `GetAuthorName` also demonstrate an idempotent write and a single-column result.

`ListAuthorsWithoutBio` uses `SELECT * WITHOUT bio` to return the author ID and name without loading the nullable biography; the generated row type contains only those selected columns.

Compared with [upstream](../README.md), IDs are explicit `Uint64` inputs rather than `SERIAL`/auto-increment values. YDB's `INSERT ... RETURNING` supplies the created row; there is no `LastInsertId` or `:execresult` contract. `bio` remains nullable. Most queries use named parameters whose types are inferred from their uses.

`FindAuthorsByName` uses the synchronous `by_name` secondary index and fetches `bio` from the base table. `FindAuthorsByNameCovering` uses `by_name_covering`, which includes `bio` in `COVER`; the primary key is included in both indexes automatically. Both queries return the full author row, preserve its nullable biography, and demonstrate qualified and unqualified wildcards with `VIEW`. The covering query declares its parameter explicitly. These queries do not change transaction ownership or consistency settings.

`ListAuthorsPage` declares `$page_size AS Int` and `$offset AS Uint32`. `Int` resolves to YDB `Int32`, and the generated bindings preserve the declared `Int32` and `Uint32` parameter types. The query orders by the primary key for deterministic pages. Use nonnegative page sizes for ordinary pagination. Signed values are bound unchanged, leaving `LIMIT`/`OFFSET` evaluation to YDB.

`FindAuthorsByNamePrefix` constructs a `LIKE` pattern in YQL and returns a Boolean `has_bio` projection. `GetAuthorStatistics` counts all authors, authors with a biography, and authors whose biography is nonempty. `COUNT_IF` ignores NULL predicates; its counts remain zero on empty input. The final `CAST(COUNT(*) AS Bool)` deliberately omits `AS` to demonstrate YDB's generated result-column name.

`GetAuthorExportMetadata` obtains the current UTC date, datetime and timestamp in YDB, exports the date/datetime as text and the timestamp as native, text and integer-microsecond values, and returns JSON export metadata. Its unaliased `COALESCE(CAST(id AS Uint32), 0)` demonstrates a fitting integer fallback: an ID outside the Uint32 range produces zero. These values are computed by YDB inside the request, without client-side clock substitution or conversion. The explicit date/datetime text conversion keeps this example within the scalar result types supported by every runtime profile.

`EchoAuthorIDText` returns a text value supplied by the caller. Its `$author_id` parameter has no inferable SQL type, so `analyzer.parameters` assigns `Utf8` to this query alone; the other author queries still infer `$author_id` as `Uint64` from the table column.

This example covers every built-in language/runtime. Each language's build files and executable smoke tests live in its own directory. See [development](../../.agents/development.md) for generation and sequential live acceptance commands.
