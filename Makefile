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
	trap 'exit 0' INT TERM; go run ./cmd/mydb server --db mydb.db --addr :5433 --seed seed.sql

sql:
	go run ./cmd/mydb sql --addr :5433

build:
	go build -o bin/mydb ./cmd/mydb
