package kv

import "testing"

func TestSecondaryIndexFindAndUpdate(t *testing.T) {
	s, err := Open(t.TempDir() + "/table.db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := NewTable(s, Schema{Columns: []Column{{"id", IntType}, {"name", StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	if err := tbl.Insert("1", Row{"id": 1, "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := tbl.Insert("2", Row{"id": 2, "name": "Bob"}); err != nil {
		t.Fatal(err)
	}
	rows, err := tbl.Find("name", "Ada")
	if err != nil || len(rows) != 1 || rows[0]["id"] != 1 {
		t.Fatalf("Find: %#v, %v", rows, err)
	}
	if _, err = tbl.Update(func(r Row) bool { return r["id"] == 1 }, Row{"name": "Eve"}); err != nil {
		t.Fatal(err)
	}
	rows, err = tbl.Find("name", "Ada")
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale index: %#v, %v", rows, err)
	}
	rows, err = tbl.Find("name", "Eve")
	if err != nil || len(rows) != 1 {
		t.Fatalf("updated index: %#v, %v", rows, err)
	}
}

func TestSecondaryIndexRebuildsOnReopen(t *testing.T) {
	path := t.TempDir() + "/table.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := NewTable(s, Schema{Columns: []Column{{"id", IntType}, {"name", StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tbl.Insert("1", Row{"id": 1, "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if err := tbl.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenTable(path, Schema{Columns: []Column{{"id", IntType}, {"name", StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.Find("name", "Ada")
	if err != nil || len(rows) != 1 || rows[0]["id"] != 1 {
		t.Fatalf("reopened index: %#v, %v", rows, err)
	}
}

func TestTableTypedRows(t *testing.T) {
	path := t.TempDir() + "/table.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := NewTable(s, Schema{Columns: []Column{{"age", IntType}, {"name", StringType}, {"active", BoolType}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tbl.Insert("1", Row{"age": 42, "name": "Ada", "active": true}); err != nil {
		t.Fatal(err)
	}
	r, found, err := tbl.Get("1")
	if err != nil || !found {
		t.Fatalf("Get: %v, %v", err, found)
	}
	if r["age"] != 42 || r["name"] != "Ada" || r["active"] != true {
		t.Fatalf("unexpected row: %#v", r)
	}
	if err := tbl.Insert("bad", Row{"age": "42", "name": "Ada", "active": true}); err == nil {
		t.Fatal("expected type error")
	}
	if err := tbl.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTableMissingColumns(t *testing.T) {
	s, err := Open(t.TempDir() + "/table.db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := NewTable(s, Schema{Columns: []Column{{"x", StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	if err := tbl.Insert("1", Row{}); err == nil {
		t.Fatal("expected missing column error")
	}
}
