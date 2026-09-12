# TypeScript target

The built-in TypeScript target generates a typed Node.js ESM module for the modular YDB JavaScript SDK. Configure it with:

```yaml
gen:
  typescript:
    out: typescript/native
    runtime: ydb
```

`runtime` defaults to `ydb`; no legacy `ydb-sdk` runtime is generated. The application creates and owns a `Driver`, then passes the SDK query function (`SQL`) to the generated class:

```ts
import { Driver } from "@ydbjs/core";
import { query } from "@ydbjs/query";
import { Queries } from "./typescript/native/queries.js";

const connectionString = process.env.YDB_CONNECTION_STRING;
if (!connectionString) throw new Error("YDB_CONNECTION_STRING is required");

const driver = new Driver(connectionString);
await driver.ready();
const client = query(driver);
const queries = new Queries(client);

const author = await queries.getAuthor(42n);
```

The constructor accepts the SDK's `SQL` type, so the same generated class can also run inside a caller-owned transaction:

```ts
await client.transaction(async (tx, signal) => {
  const queries = new Queries(tx);
  return queries.getAuthor(42n, (stmt) => { stmt.signal(signal); });
});
```

Every method has a final optional `ConfigureQuery` callback. It receives the fully parameterized SDK `Query` before execution. Use it for SDK controls such as `signal`, `timeout`, `idempotent`, `isolation` and `withStats`. The callback returns `void`; awaiting the statement starts execution.

The generator writes one `queries.ts` file containing the implementation and its exported parameter and result type aliases. SQL appears directly in indented tagged templates such as `sql<[GetAuthorRow]>\`SELECT ...\``. The analyzer supplies SQL with top-level `DECLARE` statements removed: `@ydbjs/query` reconstructs those declarations from the explicit typed values passed through `.parameter()`. Comments, string literals and non-parameter local bindings are preserved. SQL source lines are indented within the method, and lines emptied by declaration removal are omitted.

A method with one parameter accepts that value directly. Methods with multiple parameters accept a named object. `:one` returns the first typed row, or `null` when YDB returns no rows. `:many` returns an array, including an empty array for no rows. `:exec` resolves to `undefined`. `:execrows` is rejected because the SDK does not expose a portable affected-row count. Result types describe the SDK's decoded rows; generated code adds no runtime row validators. Result properties use the exact column names returned by YDB, including explicit SQL aliases. Names that require quoting are emitted as quoted TypeScript property keys. Rows are returned directly from the SDK, with no generated property mapping or SQL alias rewriting. Method and input parameter names remain camelCase.

`Int64` and `Uint64` use `bigint`, including the complete Uint64 range. Smaller integers, `Float` and `Double` use `number`. YQL `Utf8` is a TypeScript `string`; binary YQL `String` is `Uint8Array`. `Json` and `JsonDocument` accept JSON text as `string` and return SDK-decoded `JSValue`. Optional values use `null` and explicit SDK `Optional` wrappers with the element type when binding parameters. Timestamp parameters and results use `Date`, with millisecond precision. JSON decoding follows `JSON.parse`, including JavaScript number precision. The generator uses SDK value constructors directly and adds no duplicate runtime parameter validators.

The verified dependencies are `@ydbjs/core` 6.3.1, `@ydbjs/query` 6.3.0 and `@ydbjs/value` 6.0.8. The example harness compiles generated sources with TypeScript 5.9.3 before loading the emitted ESM. The SDK packages require Node.js 20.19 or newer and npm 10 or newer. The example lockfile is shared from `examples/`; generated packages do not copy runtime manifests.

The offline check type-checks every generated example module against the pinned SDK and verifies representative typed bindings. The smoke command runs all five example families sequentially against an already-running disposable YDB. Commands are in [development](../.agents/development.md#generated-runtime-checks).

Primary references: the YDB documentation for [installing the JavaScript SDK](https://ydb.tech/docs/en/reference/ydb-sdk/install), the [`@ydbjs/query` API](https://github.com/ydb-platform/ydb-js-sdk/tree/main/packages/query), and the SDK [`@ydbjs/value` implementation](https://github.com/ydb-platform/ydb-js-sdk/tree/main/packages/value).
