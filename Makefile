.PHONY: build test generate check clean

build:
	go build -trimpath -o bin/sqlc-ydb ./cmd/sqlc-ydb

test:
	go test -p 1 ./...

generate:
	go run ./cmd/sqlc-ydb generate -f examples/authors/sqlc.yaml

check:
	go test -p 1 ./...
	go run ./cmd/sqlc-ydb diff -f examples/authors/sqlc.yaml

clean:
	rm -f bin/sqlc-ydb
