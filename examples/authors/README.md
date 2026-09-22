# Authors

The introductory sqlc workflow: create an author, fetch one row, list rows ordered by name, and delete a row. `CreateAuthor` returns the inserted row. The existing `UpsertAuthor` and `GetAuthorName` queries also demonstrate an idempotent write and a single-column result.

Compared with [upstream](../README.md), IDs are explicit `Uint64` inputs rather than `SERIAL`/auto-increment values. YDB's `INSERT ... RETURNING` supplies the created row; there is no `LastInsertId` or `:execresult` contract. `bio` remains nullable. Queries use named parameters whose types are inferred from their uses.

This example covers every built-in language/runtime. Each language's build files and executable smoke tests live in its own directory. The other upstream example families focus on Go, as upstream does. See [development](../../.agents/development.md) for generation and sequential live acceptance commands.

`FindAuthorsByName` uses the synchronous `by_name` secondary index and fetches `bio` from the base table. `FindAuthorsByNameCovering` uses `by_name_covering`, which includes `bio` in `COVER`; the primary key is included in both indexes automatically. Both queries return the full author row, preserve its nullable biography, and demonstrate qualified and unqualified wildcards with `VIEW`. The covering query declares its parameter explicitly. These queries do not change transaction ownership or consistency settings.
