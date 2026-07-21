GO_BIN := $(shell go env GOPATH)/bin
GOFUMPT := $(GO_BIN)/gofumpt
GOLANGCI_LINT := $(GO_BIN)/golangci-lint

.PHONY: test fmt lint start sql build

test:
	go test ./...

fmt:
	$(GOFUMPT) -w $$(find . -name '*.go' -type f)

lint:
	$(GOLANGCI_LINT) run

start:
	trap 'exit 0' INT TERM; go run ./cmd/go-db server --db db/go-db.db --addr :5433 --seed seed.sql

sql:
	go run ./cmd/go-db sql --addr :5433

build:
	go build -o bin/go-db ./cmd/go-db
