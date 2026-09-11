# Rust target

The built-in Rust target generates asynchronous code for the official
[`ydb`](https://crates.io/crates/ydb) crate. The supported runtime value is
`ydb`; omitting `runtime` selects it.

Generated `Queries` borrows a caller-owned mutable `ydb::QueryClient`. The
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
it reports `YdbError::NoRows` for an empty result. These one-shot SDK operations acquire their own session,
choose the server-side transaction mode, drain the response, and apply the
SDK retry policy. A generated method does not start or accept an interactive
transaction. Applications that need several statements in one atomic
transaction should keep that transaction orchestration in application code.

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
