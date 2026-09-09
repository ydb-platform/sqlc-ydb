.PHONY: build test test-release generate check check-examples clean

EXAMPLE_CONFIGS := $(wildcard examples/*/sqlc.yaml)

build:
	go build -trimpath -o bin/sqlc-ydb ./cmd/sqlc-ydb

test:
	go test -p 1 ./...

test-release:
	python3 -m unittest discover -s .github/scripts/tests -p 'test_*.py'

generate: build
	@set -e; for config in $(EXAMPLE_CONFIGS); do \
		./bin/sqlc-ydb generate -f "$$config"; \
	done

check: test-release
	go test -p 1 ./...
	$(MAKE) check-examples

check-examples: build
	@set -e; for config in $(EXAMPLE_CONFIGS); do \
		./bin/sqlc-ydb compile -f "$$config"; \
		./bin/sqlc-ydb diff -f "$$config"; \
	done
	cd examples && go test -p 1 ./...
	python3 -m compileall -q examples/authors/python

clean:
	rm -f bin/sqlc-ydb
