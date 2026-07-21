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

func TestPreparedStatementBindsParameters(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(map[string]*kv.Table{"users": tbl})
	insert, err := e.Prepare("INSERT INTO users (id, name) VALUES (?, ?)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insert.Execute(1, "Ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := insert.Execute(2, "Bob"); err != nil {
		t.Fatal(err)
	}
	selectOne, err := e.Prepare("SELECT name FROM users WHERE id = ?")
	if err != nil {
		t.Fatal(err)
	}
	r, err := selectOne.Execute(2)
	if err != nil || len(r.Rows) != 1 || r.Rows[0]["name"] != "Bob" {
		t.Fatalf("prepared select = %#v, %v", r.Rows, err)
	}
	if _, err := selectOne.Execute(); err == nil {
		t.Fatal("expected parameter count error")
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

func TestSQLExplicitTransactions(t *testing.T) {
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
	if _, err := e.Execute("BEGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute("INSERT INTO users (id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	r, err := e.Execute("SELECT * FROM users")
	if err != nil || len(r.Rows) != 0 {
		t.Fatalf("rollback did not undo write: %#v %v", r, err)
	}

	if _, err := e.Execute("BEGIN; INSERT INTO users (id) VALUES (2); COMMIT;"); err != nil {
		t.Fatal(err)
	}
	r, err = e.Execute("SELECT * FROM users")
	if err != nil || len(r.Rows) != 1 || r.Rows[0]["id"] != 2 {
		t.Fatalf("commit did not preserve write: %#v %v", r, err)
	}
}

func TestSQLTransactionReadsSnapshot(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	if err := tbl.Insert("1", kv.Row{"id": 1, "name": "before"}); err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(map[string]*kv.Table{"users": tbl})
	if _, err := e.Execute("BEGIN"); err != nil {
		t.Fatal(err)
	}
	if err := tbl.Insert("1", kv.Row{"id": 1, "name": "after"}); err != nil {
		t.Fatal(err)
	}
	r, err := e.Execute("SELECT name FROM users")
	if err != nil || len(r.Rows) != 1 || r.Rows[0]["name"] != "before" {
		t.Fatalf("snapshot read = %#v, err=%v", r.Rows, err)
	}
	if _, err := e.Execute("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLTransactionReadsOwnWrites(t *testing.T) {
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
	if _, err := e.Execute("BEGIN; INSERT INTO users (id) VALUES (1); SELECT * FROM users"); err != nil {
		t.Fatal(err)
	}
	r, err := e.Execute("SELECT * FROM users")
	if err != nil || len(r.Rows) != 1 || r.Rows[0]["id"] != 1 {
		t.Fatalf("own write read=%#v err=%v", r.Rows, err)
	}
	if _, err := e.Execute("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLTransactionWriteConflict(t *testing.T) {
	s, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := kv.NewTable(s, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Close()
	if err := tbl.Insert("1", kv.Row{"id": 1, "name": "original"}); err != nil {
		t.Fatal(err)
	}
	one := NewExecutor(map[string]*kv.Table{"users": tbl})
	two := NewExecutor(map[string]*kv.Table{"users": tbl})
	if _, err := one.Execute("BEGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Execute("BEGIN"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Execute("UPDATE users SET name = 'one' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Execute("COMMIT"); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Execute("UPDATE users SET name = 'two' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Execute("COMMIT"); err == nil {
		t.Fatal("expected write conflict")
	}
	if !two.InTransaction() {
		t.Fatal("conflicted transaction should remain open")
	}
	if _, err := two.Execute("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLTransactionControlErrors(t *testing.T) {
	e := NewExecutor(nil)
	if _, err := e.Execute("COMMIT"); err == nil {
		t.Fatal("expected COMMIT outside transaction to fail")
	}
	if _, err := e.Execute("ROLLBACK"); err == nil {
		t.Fatal("expected ROLLBACK outside transaction to fail")
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
