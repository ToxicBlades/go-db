package kv

import (
	"fmt"
	"testing"
)

func TestBTreeSplitsAndMerges(t *testing.T) {
	var tree btree
	for i := 0; i < 2000; i++ {
		k := []byte(fmt.Sprintf("%04d", i))
		tree.put(k, k)
	}
	for i := 0; i < 2000; i += 2 {
		k := []byte(fmt.Sprintf("%04d", i))
		if !tree.delete(k) {
			t.Fatalf("delete %s", k)
		}
	}
	for i := 1; i < 2000; i += 2 {
		k := []byte(fmt.Sprintf("%04d", i))
		v, ok := tree.get(k)
		if !ok || string(v) != string(k) {
			t.Fatalf("missing %s", k)
		}
	}
	for i := 1; i < 2000; i += 2 {
		k := []byte(fmt.Sprintf("%04d", i))
		tree.delete(k)
	}
	if tree.root != nil {
		t.Fatal("expected empty tree")
	}
}
