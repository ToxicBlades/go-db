package sql

import (
	"mydb/kv"
	"testing"
)

func TestSQLInsertSelectWhere(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}, {Name: "active", Type: kv.BoolType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	e := NewExecutor(map[string]*kv.Table{"users": tbl})
	for _, q := range []string{"INSERT INTO users (id, name, active) VALUES (1, 'Ada', true)", "INSERT INTO users (id, name, active) VALUES (2, 'Bob', false)"} {
		if r, err := e.Execute(q); err != nil || r.Affected != 1 {
			t.Fatalf("insert: %#v %v", r, err)
		}
	}
	r, err := e.Execute("SELECT name, active FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 || r.Rows[0]["name"] != "Ada" || r.Rows[0]["active"] != true {
		t.Fatalf("unexpected result: %#v", r)
	}
}

func TestSQLRejectsUnsupported(t *testing.T) {
	if _, err := Parse("DELETE FROM users"); err == nil {
		t.Fatal("expected parse error")
	}
}
