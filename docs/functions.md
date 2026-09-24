# Function signatures

`sqlc-ydb` resolves function result types offline. The shipped catalog covers the named functions in the [YDB YQL UDF module list](https://ydb.tech/docs/ru/yql/reference/udf/list/?version=main) with documented, representable type contracts. An accepted call still needs an output type supported by the selected generator; a function being available on a YDB installation does not by itself establish its offline type contract.

## Configured functions

Applications can declare concrete signatures for custom UDFs under `sql[].analyzer.functions`:

```yaml
version: "2"
sql:
- engine: ydb
  schema: schema.sql
  queries: queries.sql
  analyzer:
    functions:
    - name: Acme::Score
      args:
      - name: value
        type: Utf8
        auto_map: true
      - name: mode
        type: Uint32
        optional: true
      returns: Double
  gen:
    go:
      out: db
```

`type` and `returns` use canonical YQL type syntax. Each configured overload must use concrete argument and return types; `Any`, `Null` anywhere in the type, nested `Optional<Optional<T>>`, type variables, and server-assisted discovery are not supported. Ordinary concrete container and Struct types remain valid when all nested types meet these rules. Dictionary keys follow YQL's key contract: a supported primitive scalar, a tuple of at least two valid keys, or one optional level around a valid key; `Json`, `JsonDocument`, `Yson`, `Null`, `Void`, consecutive optional levels, and non-tuple container keys are rejected recursively. `optional: true` means that a trailing argument may be omitted. A `name` permits YQL named-argument syntax. `auto_map: true` accepts the optional form of that exact argument type and propagates its nullability to the declared return type.

`optional` controls whether an argument may be left out; it does not make the argument value nullable. Use an `Optional<T>` argument type when the function accepts an optional value. AutoMap must be declared on the base type `T`, not `Optional<T>`: it lifts `T` and `Optional<T>` calls and, when the input is `NULL`, returns `NULL` without invoking the function. Consequently an optional call wraps any non-optional declared result, including a concrete container or Struct, in one `Optional` level. This follows YQL's [callable argument flag contract](https://ydb.tech/docs/en/yql/reference/types/type_string). A direct integer literal may fit a different integer parameter width if its value is in range; typed nonliteral arguments otherwise require the declared type.

Configured signatures are trusted type contracts for generation. They neither install nor execute a UDF and do not verify that the function exists on the destination server. Deployment and availability remain the application's responsibility. Configuration fails before query analysis for invalid types, ambiguous duplicate overloads, or a custom declaration that conflicts with a known built-in signature.

The shipped `Yson::ConvertToStringList` rule is intentionally separate from configured AutoMap. YDB implicitly converts serialized optional `Json` or `Yson` to its internal resource and reports a required `List<String>` result even for a `NULL` source; this verified wrapper behavior is not generalized to user signatures.

## YQL UDF modules

The catalog covers the documented named functions in DateTime, Digest, Histogram, Hyperscan, Ip, Knn, Math, Pcre, Pire, Re2, Roaring, String, Unicode, Url and Yson. `Json::From` also resolves through the YSON-compatible value contract. For example, `Digest::CityHash` accepts a string with AutoMap and an optional named `Init` seed; optional string input produces an optional hash. The regex modules and DateTime parsers/formatters can produce callable values, which may be assigned to a local `$name` and invoked later in the same query.

`Resource`, `Tagged`, `Callable` and local `Lambda` are intermediate YQL types: use a UDF such as `Yson::SerializeJson` or `Math::NearbyInt` before returning a result column. `Yson::ConvertTo(node, Type)` requires a literal YQL target type. `Yson::Parse`, `Yson::ParseJson` and `Yson::ParseJsonDecodeUtf8` accept String, Utf8, supported serialized JSON/YSON inputs and a YSON node resource; string and NULL inputs return an optional resource. `Pire`/`Hyperscan`/`Pcre` multi-pattern functions and `Re2::Capture` require a direct pattern literal so the offline analyzer can determine the exact tuple width or Struct fields; a dynamic pattern produces a diagnostic. The histogram functions accept the typed result of `HISTOGRAM` and its supported aliases. See the [booktest example](../examples/booktest/queries.sql) for scalar UDF calls and grouped YSON serialization.
