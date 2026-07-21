.PHONY: test start build

test:
	go test ./...

start:
	go run ./cmd/mydb mydb.db

build:
	go build -o bin/mydb ./cmd/mydb