GO_BIN := $(shell go env GOPATH)/bin
GOFUMPT := $(GO_BIN)/gofumpt
GOLANGCI_LINT := $(GO_BIN)/golangci-lint

.PHONY: help test fmt lint start sql build docker-build

help:
	@echo "Available targets:"
	@echo "  help          Show this help message"
	@echo "  test          Run tests"
	@echo "  fmt           Format Go files"
	@echo "  lint          Run the linter"
	@echo "  start         Start the database server"
	@echo "  sql           Connect to the database with the SQL client"
	@echo "  build         Build the go-db binary"
	@echo "  docker-build  Build the Docker image"

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

docker-build:
	docker build -t go-db .
