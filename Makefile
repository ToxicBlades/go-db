.PHONY: test start sql build

test:
	go test ./...

start:
	go run ./cmd/mydb server --db mydb.db --addr :5433 --seed seed.sql

sql:
	go run ./cmd/mydb sql --addr :5433

build:
	go build -o bin/mydb ./cmd/mydb
