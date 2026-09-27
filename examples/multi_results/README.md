# Multiple result sets

`FetchSummary` returns three independently typed result sets from one YDB query request. The optional `-- result:` comments name the first two fields; the third uses `Result3`. Every field is a slice, including the empty first result and the second SELECT with `LIMIT 1`.

```go
summary, err := multires.New(executor).FetchSummary(ctx, 42)
if err != nil {
    return err
}
fmt.Println(len(summary.Item), summary.Flags[0].Enabled, summary.Result3[0].Status)
```

Both generated Go profiles are in [go/native](go/native) and [go/database/sql](go/database/sql). The caller supplies a native Query Service executor or a `database/sql` connection and retains its transaction and retry policy. The generated method reads every result set, checks its names and YQL types, closes the result, and returns no partial rows on an error. Java JDBC output is in [java/jdbc](java/jdbc); its borrowed connection retains transaction and retry ownership, and its result record has non-null typed lists for every set. Other target runtimes reject `:multi` during generation.

`BareLiterals` uses the three unaliased literal SELECTs from the original request. It generates `Result1`, `Result2` and `Result3`, each with its own row type.

The Java [live smoke](../../tests/examples/java/batch/src/test/java/MultiSmoke.java) calls both queries in buffered and streaming JDBC modes.
