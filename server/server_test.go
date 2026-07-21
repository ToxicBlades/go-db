package server

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"mydb/kv"
	"mydb/sql"
)

func TestProtocol(t *testing.T) {
	store, err := kv.Open(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	table, err := kv.NewTable(store, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}}})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := New(sql.NewExecutor(map[string]*kv.Table{"users": table}))
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go s.handle(a)
	client := bufio.NewReader(b)
	if _, err = b.Write([]byte(`{"query":"INSERT INTO users (id, name) VALUES (1, 'Ada')"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	var r Response
	if err = json.NewDecoder(client).Decode(&r); err != nil || !r.OK || r.Affected != 1 {
		t.Fatalf("insert response: %#v %v", r, err)
	}
	if _, err = b.Write([]byte("SELECT * FROM users\n")); err != nil {
		t.Fatal(err)
	}
	if err = json.NewDecoder(client).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if !r.OK || len(r.Rows) != 1 || r.Rows[0]["name"] != "Ada" {
		t.Fatalf("select response: %#v", r)
	}
	if _, err = b.Write([]byte("nonsense\n")); err != nil {
		t.Fatal(err)
	}
	if err = json.NewDecoder(client).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.OK || !strings.Contains(r.Error, "expected SELECT") {
		t.Fatalf("error response: %#v", r)
	}
}
