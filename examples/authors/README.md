# Authors

The introductory sqlc workflow: create an author, fetch one row, list rows ordered by name, page through rows ordered by ID, and delete a row. `CreateAuthor` returns the inserted row. `UpsertAuthor` and `GetAuthorName` also demonstrate an idempotent write and a single-column result.

Compared with [upstream](../README.md), IDs are explicit `Uint64` inputs rather than `SERIAL`/auto-increment values. YDB's `INSERT ... RETURNING` supplies the created row; there is no `LastInsertId` or `:execresult` contract. `bio` remains nullable. Most queries use named parameters whose types are inferred from their uses.

`ListAuthorsPage` declares `$page_size AS Int` and `$offset AS Uint32`. `Int` resolves to YDB `Int32`, and the generated bindings preserve the declared `Int32` and `Uint32` parameter types. The query orders by the primary key for deterministic pages. Use nonnegative page sizes for ordinary pagination. Signed values are bound unchanged, leaving `LIMIT`/`OFFSET` evaluation to YDB.

This example covers every built-in language/runtime. Each language's build files and executable smoke tests live in its own directory. See [development](../../.agents/development.md) for generation and sequential live acceptance commands.
