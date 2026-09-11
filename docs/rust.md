# Rust target

The built-in Rust target generates asynchronous code for the official
[`ydb`](https://crates.io/crates/ydb) crate. The supported runtime value is
`ydb`; omitting `runtime` selects it.

Generated `Queries<E>` borrows a caller-owned mutable `E: ydb::QueryExecutor`,
which can be a `ydb::QueryClient` or a `ydb::Transaction`. The
application owns the YDB client and creates the query client:

```rust
let client = ydb::ClientBuilder::new_from_connection_string(connection_string)?
    .build()
    .await?;
let mut query_client = client.query_client();
let mut queries = generated::queries::Queries::new(&mut query_client);
```

Each generated method executes one statement through the Query Service API.
`:exec` uses `QueryClient::exec`; `:one` uses `query_row`, and `:many` uses
`query_result_set`. `:one` returns the first row, matching the sqlc contract;
it reports `YdbError::NoRows` for an empty result. With a `QueryClient`, methods
use the SDK's one-shot operations and retry policy. With a `Transaction`, all
methods execute in that transaction. Generated code does not begin, commit,
roll back, or retry transactions; the caller controls their lifetime.

Use `retry_tx` to execute several generated queries atomically:

```rust
query_client
    .retry_tx(ydb::closure!(async |tx| {
        let mut queries = generated::queries::Queries::new(tx);
        let author = queries.create_author()
            .author_id(1).name("Ada").biography(None).call().await?;
        queries.create_book()
            .book_id(1).author_id(author.author_id)
            .isbn("isbn-1").book_type("FICTION").title("Typed Rust")
            .year(2026).available(std::time::SystemTime::UNIX_EPOCH)
            .tags("[]").call().await?;
        Ok(())
    }))
    .await?;
```

This example uses the `batch` schema. The SDK commits on callback success and
rolls back on failure. A retry runs the entire callback again, so keep external
side effects outside it.

The generated API maps the YQL types used by the bundled examples as follows:

| YQL | Rust |
| --- | --- |
| `Bool` | `bool` |
| signed and unsigned integers | matching `i8`-`i64` or `u8`-`u64` |
| `Float`, `Double` | `f32`, `f64` |
| `Utf8` | `String` |
| `String`, `Yson` | `ydb::Bytes` |
| `Json`, `JsonDocument` | `String` |
| date/time types including `Timestamp` | `std::time::SystemTime` |
| `Optional<T>` | `Option<T>` |

JSON parameters use generated private wrappers before entering the SDK. This
is required because a plain Rust `String` maps to YDB `Utf8`, while JSON must
map to `ydb::Value::Json` or `ydb::Value::JsonDocument`. The wrapper also
implements `Default`, allowing the SDK's public `Option<T>` conversion to
construct a correctly typed null for `Optional<Json>` and
`Optional<JsonDocument>`.

Result values are read by resolved projection position through `Row::remove_field`
and decoded with the SDK's `TryFrom<ydb::Value>` implementations. This also
handles qualified result names produced by joins. Missing columns, nullability
mismatches, unexpected wire types and no rows for `:one`
are returned as `ydb::YdbError`; generated code does not substitute
defaults.

`:many` decodes rows with a fallible iterator collected into a `Vec`, returning
the first decoding error. Result structs also derive `Copy` when all their fields
are copyable, including optional scalars and timestamps.

SQL is emitted as readable Rust raw strings without `DECLARE` statements because
the generated typed SDK parameters supply their YQL types. The delimiter grows
when the SQL contains quote/hash sequences, and control bytes that Rust source
cannot hold literally are represented with `concat!` and byte escapes. Apart
from declaration tokens and their whitespace-only placeholder lines, the
runtime string preserves the analyzed SQL bytes. Generated Rust source passes
`rustfmt --check` without a formatting rewrite.

The shared examples pin `ydb` 0.18.2 and require Rust 1.88 or newer, matching
the SDK's published minimum supported Rust version. `:execrows`, container
types, decimal values, UUID values, and other unmapped YQL types produce a
generation error.
Query names that normalize to `new` are rejected because `Queries::new` is the
generated constructor; choose a different query annotation name.

All query methods use type-safe `bon` builders, including parameterless queries.
Add `bon = "3.10.1"` to the consuming crate's dependencies.

```rust
let author = queries.author().author_id(1).call().await?;
let authors = queries.list_authors().call().await?;
```

Every SQL parameter must be set before `.call()` is available. String setters
accept `Into<String>` (including `&str`); numeric setters retain concrete types.
Optional setters accept `Into<Option<T>>`: pass `None`, `Some(value)`, or a `T`
value directly. Conversion applies to the whole option, so `None` needs no type
annotation. Nullable parameters must still be explicitly set, including nulls.

For a list parameter, use `WHERE id IN $ids` (or `NOT IN $ids`). The analyzer
infers the list element type from the column; `DECLARE $ids AS List<Uint64>`
is also accepted. `IN ($ids)` instead contains one scalar parameter.

List setters accept `IntoIterator<Item = impl Borrow<T>>`, where `T` is the
resolved Rust element type:

```rust
queries.find().ids(vec![1u64, 2]).call().await?;
queries.find().ids(&[1u64, 2][..]).call().await?;
queries.find().ids(std::collections::HashSet::from([1u64, 2])).call().await?;
queries.find().ids((1u64..10).filter(|id| id % 2 == 0)).call().await?;
queries.find().ids(Vec::<u64>::new()).call().await?;
```

The iterator is collected into a typed SDK list when the query executes. Empty
lists retain their SQL element type. Lists support the scalar types in the table
above; nested lists, nullable lists and nullable list elements are unsupported.
