# C++ generation

The built-in C++ generator emits C++20 for two runtimes:

- `runtime: ydb` uses the native YDB C++ Query SDK.
- `runtime: userver` uses userver's asynchronous YDB driver.

Each output contains `models.hpp`, `queries.hpp`, and `queries.cpp`. The configured namespace may contain nested components such as `authors::native`. Generated query methods implement `:one`, `:many`, and `:exec`. A `:one` method returns `std::optional<Row>` and reads the first row, so an empty result becomes `std::nullopt`. A `:many` method returns `std::vector<Row>`. An `:exec` method checks execution success and intentionally discards any result sets. `:execrows` and unsupported types are rejected during generation.

## Configuration

```yaml
gen:
  cpp:
    namespace: authors::native
    out: cpp/native
    runtime: ydb
```

Use `runtime: userver` for the userver adapter. The configuration layer also accepts `runtime: native` as an alias for `ydb`; the generator's canonical runtime names are `ydb` and `userver`.

## Ownership and transactions

`Queries` stores a non-owning pointer to the runtime executor. The caller must keep the supplied client or transaction actor alive longer than the generated `Queries` object. The native driver and userver component remain caller-owned as well.

Construct native `Queries` with `TQueryClient&` for standalone calls. Every method then calls `RetryQuerySync`, obtains a retry-managed `TSession`, and executes one query with `BeginTx(SerializableRW()).CommitTx()`. Every retry attempt rebuilds the parameter object. Construct it with `TTransaction&` to run several generated methods in that caller-owned transaction; methods use `TTxControl::Tx(transaction)` and never commit, roll back or retry it.

Construct userver `Queries` with `TableClient&` for standalone calls through `TableClient::ExecuteQuery`. To run several generated methods atomically, create `Queries` from the `TxActor&` supplied to `TableClient::RetryTx`. The callback controls commit or rollback through its returned `TxAction`; a retry repeats the whole callback.

## Types

| YQL | Native SDK C++ | userver C++ |
| --- | --- | --- |
| `Bool` | `bool` | `bool` |
| `Int8` / `Uint8` | `std::int8_t` / `std::uint8_t` | same |
| `Int16` / `Uint16` | `std::int16_t` / `std::uint16_t` | same |
| `Int32` / `Uint32` | `std::int32_t` / `std::uint32_t` | same |
| `Int64` / `Uint64` | `std::int64_t` / `std::uint64_t` | same |
| `Float` | `float` | unsupported by userver |
| `Double` | `double` | `double` |
| `String` | `std::string` through SDK `String` accessors | `std::string` |
| `Utf8` | `std::string` through SDK `Utf8` accessors | `userver::ydb::Utf8` |
| `Optional<T>` | `std::optional<T>` | `std::optional<T>` |

The native mapping retains the `String` versus `Utf8` distinction in its parameter builders and result parsers even though both values use `std::string`. userver uses its strong `Utf8` typedef, so the distinction is also visible in the public C++ type. Nested optionals and non-scalar containers are rejected explicitly.

Identifiers must be ASCII C++ identifiers, must not be C++20 keywords, and must not start with `_` or the generator-reserved `sqlc_` prefix. Duplicate query, parameter, or result-column names are rejected, as are names that collide with generated row types, the `Queries` class, its client member, or per-query SQL constants. Generated SQL normally remains readable as a multiline raw string. The generator selects a raw-string delimiter absent from the SQL and switches to length-preserving escaped fragments for control bytes, carriage returns, byte-order marks, and invalid UTF-8.

## Dependencies and examples

Generated code requires C++20. Native applications link the SDK `Driver`,
`Params`, and `Query` components; userver applications link `userver::ydb`.
The bundled [CMake project](../examples/authors/cpp/CMakeLists.txt) builds both
profiles. Its [container environment](../examples/authors/cpp/Dockerfile) pins
YDB C++ SDK 3.21.1 and userver 3.2-rc.

See the [authors configuration](../examples/authors/sqlc.yaml) for both output
profiles. Instructions for reproducing the pinned example environment and
its packaging workarounds are in the
[maintainer build notes](../.agents/cpp-development.md#build-and-smoke).
