package main

import (
	"fmt"
	"log"
	"path/filepath"

	"go-db/kv"
)

func main() {
	store, err := kv.Open(filepath.Join("/tmp", "go-db-example.db"))
	if err != nil {
		log.Fatal(err)
	}
	table, err := kv.NewTable(store, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		log.Fatal(err)
	}
	defer table.Close()
	if err := table.Insert("1", kv.Row{"id": 1, "name": "Ada"}); err != nil {
		log.Fatal(err)
	}
	row, found, err := table.Get("1")
	if err != nil || !found {
		log.Fatal(err)
	}
	fmt.Println(row)
}
