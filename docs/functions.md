# Function signatures

`sqlc-ydb` resolves function result types offline. Its built-in catalog is deliberately bounded to signatures whose argument and result types are documented and can be represented exactly by the analyzer. A function being available in a particular YDB installation does not by itself make its type contract available to offline generation.

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

`type` and `returns` use canonical YQL type syntax. Each configured overload must use concrete argument and return types; `Any`, type variables, implicit coercion guesses, and server-assisted discovery are not supported. `optional: true` means that a trailing argument may be omitted. A `name` permits YQL named-argument syntax. `auto_map: true` accepts the optional form of that exact argument type and propagates its nullability to the declared return type.

`optional` controls whether an argument may be left out; it does not make the argument value nullable. Use an `Optional<T>` argument type when the function accepts an optional value. AutoMap must be declared on the base type `T`, not `Optional<T>`: it lifts `T` and `Optional<T>` calls and, when the input is `NULL`, returns `NULL` without invoking the function. Consequently an optional call wraps any non-optional declared result, including a concrete container or Struct, in one `Optional` level. This follows YQL's [callable argument flag contract](https://ydb.tech/docs/en/yql/reference/types/type_string). No other implicit argument conversion is assumed.

Configured signatures are trusted type contracts for generation. They neither install nor execute a UDF and do not verify that the function exists on the destination server. Deployment and availability remain the application's responsibility. Configuration fails before query analysis for invalid types, ambiguous duplicate overloads, or a custom declaration that conflicts with a known built-in signature.

The shipped `Yson::ConvertToStringList` rule is intentionally separate from configured AutoMap. YDB implicitly converts serialized optional `Json` or `Yson` to its internal resource and reports a required `List<String>` result even for a `NULL` source; this verified wrapper behavior is not generalized to user signatures.

## Digest library

The shipped catalog includes a conservative scalar subset of the documented [`Digest` library](https://ydb.tech/docs/en/yql/reference/udf/list/digest). `Digest::CityHash` has the exact contract `String{Flags:AutoMap}, [Init:Uint64?] -> Uint64`; `Init` is an omittable named argument and optional string input makes the result optional.

The same documented scalar rules cover `Crc32c`, `Crc64`, `Fnv32`, `Fnv64`, `MurMurHash`, `MurMurHash32`, `MurMurHash2A`, `MurMurHash2A32`, `NumericHash`, `Md5Hex`, `Md5Raw`, `Md5HalfMix`, `FarmHashFingerprint`, `FarmHashFingerprint32`, `FarmHashFingerprint64`, `SuperFastHash`, `Sha1`, `Sha256`, `IntHash64`, and `XXH3`.

This is not the whole Digest registry. Tuple-returning 128-bit functions, keyed or salted functions, and functions with several heterogeneous arguments remain unsupported until their complete call rules are modeled and tested. The broader [YDB UDF library list](https://ydb.tech/docs/en/yql/reference/udf/list/) is an availability reference, not a claim that every listed function is in the shipped offline catalog. A listed application function with a fully known concrete signature using supported model types can be described in `analyzer.functions`; resource-valued functions, polymorphic signatures, callbacks, and type-dependent overloads remain outside this configuration contract.
