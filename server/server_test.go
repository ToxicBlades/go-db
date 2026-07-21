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

func TestAuthentication(t *testing.T) {
	s, err := NewWithAuth(sql.NewExecutor(nil), "alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go s.handle(a)
	client := bufio.NewReader(b)
	if _, err = b.Write([]byte(`{"username":"alice","password":"secret"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err = json.NewDecoder(client).Decode(&response); err != nil || !response.OK {
		t.Fatalf("authentication response: %#v %v", response, err)
	}

	c, d := net.Pipe()
	defer c.Close()
	defer d.Close()
	go s.handle(c)
	if _, err = d.Write([]byte(`{"username":"alice","password":"wrong"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err = json.NewDecoder(bufio.NewReader(d)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Error != "authentication failed" {
		t.Fatalf("failed authentication response: %#v", response)
	}
}
