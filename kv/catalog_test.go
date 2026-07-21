package kv

import (
	"path/filepath"
	"testing"
)

func TestCatalogPersistsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.catalog")
	schema := Schema{Columns: []Column{{Name: "id", Type: IntType}, {Name: "name", Type: StringType}}, Constraints: map[string]ColumnConstraint{"name": {Unique: true}}}
	catalog, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.Set(CatalogEntry{Name: "users", Path: "/tmp/users.db", Schema: schema}); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := reopened.Entries()
	if len(entries) != 1 || entries[0].Name != "users" || entries[0].Path != "/tmp/users.db" {
		t.Fatalf("unexpected catalog entries: %#v", entries)
	}
	if entries[0].Schema.Constraints["name"].Unique != true {
		t.Fatalf("schema constraints were not persisted: %#v", entries[0].Schema)
	}

	if err := reopened.Delete("users"); err != nil {
		t.Fatal(err)
	}
	if entries, err := OpenCatalog(path); err != nil || len(entries.Entries()) != 0 {
		t.Fatalf("catalog delete was not persisted: %v", err)
	}
}
