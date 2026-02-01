# pluginpb → pb

Types have moved to **`internal/codegen/pb`**. Generate them with:

```bash
make proto
```

Then use `import "github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb"` and the `pb` package in code.
