.PHONY: build test coverage test-release generate lint lint-version check check-examples clean

EXAMPLE_CONFIGS := $(wildcard examples/*/sqlc.yaml)
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION := 2.13.2

build:
	go build -trimpath -o bin/sqlc-ydb ./cmd/sqlc-ydb

test: generate
	go test -p 1 ./...

coverage: generate
	go test -p 1 -count=1 -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-release:
	python3 -m unittest discover -s .github/scripts/tests -p 'test_*.py'

generate: build
	@set -e; for config in $(EXAMPLE_CONFIGS); do \
		./bin/sqlc-ydb generate -f "$$config"; \
	done

lint-version:
	@printf 'v%s\n' '$(GOLANGCI_LINT_VERSION)'

lint:
	@test "$$($(GOLANGCI_LINT) version --short)" = "$(GOLANGCI_LINT_VERSION)" || \
		{ echo 'Install golangci-lint v$(GOLANGCI_LINT_VERSION); see .agents/development.md.' >&2; exit 1; }
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run ./...
	cd examples && $(GOLANGCI_LINT) run --config ../.golangci.yml ./...
	cd tests/examples/go && $(GOLANGCI_LINT) run --config ../../../.golangci.yml ./...

check: test-release generate lint
	go test -p 1 ./...
	$(MAKE) check-examples

check-examples: generate
	@set -e; for config in $(EXAMPLE_CONFIGS); do \
		./bin/sqlc-ydb compile -f "$$config"; \
		./bin/sqlc-ydb diff -f "$$config"; \
	done
	git diff --exit-code -- examples
	@untracked=$$(git ls-files --others --exclude-standard -- examples); \
	if [ -n "$$untracked" ]; then \
		printf 'Untracked example files must be committed:\n%s\n' "$$untracked"; \
		exit 1; \
	fi
	cd examples && go test -p 1 ./...
	cd tests/examples/go && go test -p 1 ./...
	python3 -m compileall -q examples/authors/python

clean:
	rm -f bin/sqlc-ydb
