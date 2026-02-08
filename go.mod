module github.com/sqlc-dev/sqlc-engine-ydb

go 1.24.7

require (
	github.com/antlr4-go/antlr/v4 v4.13.1
	github.com/google/go-cmp v0.7.0
	github.com/sqlc-dev/sqlc v0.0.0
	github.com/stretchr/testify v1.10.0
	github.com/ydb-platform/ydb-go-sdk/v3 v3.125.1
	github.com/ydb-platform/yql-parsers v0.0.0-20260114120254-eb0b4771c57b
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/ydb-platform/ydb-go-genproto v0.0.0-20251125145508-6d7ef87db5cb // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/grpc v1.78.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/sqlc-dev/sqlc => ../engine-plugin
