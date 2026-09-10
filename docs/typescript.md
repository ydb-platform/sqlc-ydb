# TypeScript target

The built-in TypeScript target generates a typed Node.js ESM module for the modular
YDB JavaScript SDK. Configure it with:

```yaml
gen:
  typescript:
    out: typescript/native
    runtime: ydb
```

`runtime` defaults to `ydb`; no legacy `ydb-sdk` runtime is generated. The
application creates and owns a `Driver` and `QueryClient`, then passes its `SQL`
function to the generated class:

```ts
import { Driver } from "@ydbjs/core";
import { query } from "@ydbjs/query";
import { Queries } from "./typescript/native/queries.js";

const connectionString = process.env.YDB_CONNECTION_STRING;
if (!connectionString) throw new Error("YDB_CONNECTION_STRING is required");

const driver = new Driver(connectionString);
const client = query(driver);
const queries = new Queries(client);

const author = await queries.getAuthor(42n);
```

The constructor accepts the SDK's `SQL` type, so the same generated class can
also run inside a caller-owned transaction:

```ts
await client.transaction(async (tx, signal) => {
  const queries = new Queries(tx);
  return queries.getAuthor(42n, (pending) => pending.signal(signal));
});
```

Every method has a final optional `ConfigureQuery` callback. It receives the
fully parameterized SDK `Query` before execution and before `.raw()` decoding.
Use it for SDK controls such as `signal`, `timeout`, `idempotent`, `isolation`
and `withStats`; return the configured query from the callback.

The generator writes one `queries.ts` file containing the implementation and its
exported parameter and result types. SQL
constants such as `GET_AUTHOR_SQL` contain the original analyzed query byte for
byte. For execution, the analyzer also supplies the same SQL with top-level
`DECLARE` statements removed: `@ydbjs/query` reconstructs those declarations
from the explicit typed values passed through `.parameter()`. Comments, string
literals, identifiers, whitespace and non-parameter local bindings remain
unchanged.

A method with one parameter accepts that value directly. Methods with multiple
parameters accept a named object. `:one` returns the first typed row, or `null`
when YDB returns no rows. `:many` returns an array, including an empty array for
no rows. `:exec` resolves to `undefined`. `:execrows` is rejected because the SDK
does not expose a portable affected-row count. Missing result sets, non-object
rows and absent projected columns throw an error rather than trying another row
shape.
The analyzer records exact result keys where YDB adds a qualifier, such as
`b.book_id` in a join; generated API properties retain their ordinary names.

`Int64` and `Uint64` use `bigint`, including the complete Uint64 range. Smaller
integers use `number` with generated integer and range checks. `Float` and
`Double` require finite numbers; `Float` also rejects Float32 overflow. YQL `Utf8` is a
TypeScript `string`; binary YQL `String` is `Uint8Array`. `Json` and
`JsonDocument` accept and return validated JSON text as `string`, preserving
large numeric tokens and exact object-member order rather than routing through
JavaScript numbers at runtime. Optional values use `null`. Timestamp values are `bigint`
microseconds since the Unix epoch, from `0n` through `4291747199999999n`.
Queries returning Timestamp or JSON decode every projected column from the
SDK's raw-value mode against its analyzed YQL type. This avoids
the SDK's default conversion through JavaScript `Date`, which truncates
sub-millisecond precision.

The verified dependencies are `@ydbjs/core` 6.3.1, `@ydbjs/query` 6.3.0 and
`@ydbjs/value` 6.0.8. The example harness compiles generated sources with
TypeScript 5.9.3 before loading the emitted ESM. The SDK packages require Node.js
20.19 or newer and npm 10 or newer.
The example lockfile is shared from `examples/`; generated packages do not copy
runtime manifests.

The offline check type-checks every generated example module against the pinned
SDK and verifies representative typed bindings. The smoke command runs all five
example families sequentially against an already-running disposable YDB.
Commands are in [development](../.agents/development.md#generated-runtime-checks).

Primary references: the YDB documentation for
[installing the JavaScript SDK](https://ydb.tech/docs/en/reference/ydb-sdk/install),
the [`@ydbjs/query` API](https://github.com/ydb-platform/ydb-js-sdk/tree/main/packages/query),
and the SDK [`@ydbjs/value` implementation](https://github.com/ydb-platform/ydb-js-sdk/tree/main/packages/value).
