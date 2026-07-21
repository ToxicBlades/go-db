package kv

import "testing"

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
