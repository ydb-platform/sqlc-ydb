package codegen

// Destination is the target API / language for code generation.
type Destination int

const (
	// DatabaseSQL generates Go code for database/sql (DBTX, ExecContext, QueryContext, QueryRowContext).
	DatabaseSQL Destination = iota
	// YdbGoSDK generates Go code for ydb-go-sdk (query package, ParamsBuilder, QueryRow, Exec).
	YdbGoSDK
	// YdbPythonSDK generates Python code for ydb-python-sdk (QuerySessionPool, execute_with_retries).
	YdbPythonSDK
)
