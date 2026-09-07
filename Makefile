.PHONY: build test test-release generate check clean

build:
	go build -trimpath -o bin/sqlc-ydb ./cmd/sqlc-ydb

test:
	go test -p 1 ./...

test-release:
	python3 -m unittest discover -s scripts -p 'test_*.py'

generate:
	go run ./cmd/sqlc-ydb generate -f examples/authors/sqlc.yaml

check: test-release
	go test -p 1 ./...
	go run ./cmd/sqlc-ydb diff -f examples/authors/sqlc.yaml

clean:
	rm -f bin/sqlc-ydb
