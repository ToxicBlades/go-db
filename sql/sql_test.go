package sql

import (
	"mydb/kv"
	"strings"
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
	if _, err := Parse("DELETE FROM users WHERE id ~~ 1"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSQLExplainDoesNotExecute(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	e := NewExecutor(map[string]*kv.Table{"users": tbl})
	r, err := e.Execute("EXPLAIN SELECT * FROM users WHERE id = 1;")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 || !strings.Contains(r.Rows[0]["plan"].(string), "Seq Scan on users") {
		t.Fatalf("unexpected plan: %#v", r)
	}
	if rows, err := e.Execute("SELECT * FROM users"); err != nil || len(rows.Rows) != 0 {
		t.Fatalf("EXPLAIN executed query: %#v %v", rows, err)
	}
}

func TestSQLExplainTable(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}, Constraints: map[string]kv.ColumnConstraint{"name": {NotNull: true, Unique: true}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	r, err := NewExecutor(map[string]*kv.Table{"users": tbl}).Execute("EXPLAIN TABLE users")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[1]["column"] != "name" || r.Rows[1]["type"] != "STRING" || r.Rows[1]["nullable"] != false || r.Rows[1]["unique"] != true {
		t.Fatalf("unexpected table explanation: %#v", r)
	}
}

func TestSQLWhereBooleanAndComparisons(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	e := NewExecutor(map[string]*kv.Table{"users": tbl})
	for _, q := range []string{"INSERT INTO users (id,name) VALUES (1,'Ada')", "INSERT INTO users (id,name) VALUES (2,'Bob')", "INSERT INTO users (id,name) VALUES (3,'Cid')"} {
		if _, err := e.Execute(q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.Execute("SELECT id FROM users WHERE id >= 2 AND id != 3 OR name = 'Ada'")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 {
		t.Fatalf("unexpected rows: %#v", r.Rows)
	}
}

func TestSQLSemicolonAndListTables(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	e := NewExecutor(map[string]*kv.Table{"users": tbl, "audit": tbl})
	r, err := e.Execute("SHOW TABLES;")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[0]["table_name"] != "audit" || r.Rows[1]["table_name"] != "users" {
		t.Fatalf("unexpected tables: %#v", r)
	}
	if _, err := Parse("SHOW TABLES; SELECT * FROM users;"); err == nil {
		t.Fatal("expected trailing statement to be rejected")
	}
}

func TestSQLAutoIncrementID(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := NewExecutor(map[string]*kv.Table{})
	if _, err := e.Execute("CREATE TABLE users (name STRING)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute("INSERT INTO users (name) VALUES ('Ada')"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute("INSERT INTO users (name) VALUES ('Bob')"); err != nil {
		t.Fatal(err)
	}
	r, err := e.Execute("SELECT id, name FROM users")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[0]["id"] != 1 || r.Rows[1]["id"] != 2 {
		t.Fatalf("unexpected auto-generated IDs: %#v", r.Rows)
	}
}

func TestSQLOrderLimitOffsetAndAggregates(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "team", Type: kv.StringType}, {Name: "score", Type: kv.IntType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	e := NewExecutor(map[string]*kv.Table{"scores": tbl})
	for _, q := range []string{"INSERT INTO scores (id,team,score) VALUES (1,'a',10)", "INSERT INTO scores (id,team,score) VALUES (2,'b',30)", "INSERT INTO scores (id,team,score) VALUES (3,'a',20)", "INSERT INTO scores (id,team,score) VALUES (4,'b',40)"} {
		if _, err := e.Execute(q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.Execute("SELECT id, score FROM scores ORDER BY score DESC LIMIT 2 OFFSET 1")
	if err != nil || len(r.Rows) != 2 || r.Rows[0]["id"] != 2 || r.Rows[1]["id"] != 3 {
		t.Fatalf("ordered page: %#v %v", r, err)
	}
	r, err = e.Execute("SELECT team, COUNT(*), SUM(score), AVG(score), MIN(score), MAX(score) FROM scores GROUP BY team ORDER BY team")
	if err != nil || len(r.Rows) != 2 {
		t.Fatalf("aggregate result: %#v %v", r, err)
	}
	if r.Rows[0]["team"] != "a" || r.Rows[0]["COUNT(*)"] != 2 || r.Rows[0]["SUM(score)"] != 30 || r.Rows[0]["AVG(score)"] != 15.0 || r.Rows[0]["MIN(score)"] != 10 || r.Rows[0]["MAX(score)"] != 20 {
		t.Fatalf("aggregate row: %#v", r.Rows[0])
	}
}
