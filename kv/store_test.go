package kv

import (
	"fmt"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPutAndGet(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put([]byte("hello"), []byte("world")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	val, found, err := s.Get([]byte("hello"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected key to be found")
	}
	if string(val) != "world" {
		t.Fatalf("expected 'world', got %q", val)
	}
}

func TestGetMissingKey(t *testing.T) {
	s := openTestStore(t)

	_, found, err := s.Get([]byte("nope"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected key not to be found")
	}
}

func TestOverwrite(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put([]byte("k"), []byte("v1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := s.Put([]byte("k"), []byte("v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	val, found, err := s.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected key to be found")
	}
	if string(val) != "v2" {
		t.Fatalf("expected latest write 'v2', got %q", val)
	}
}

func TestDelete(t *testing.T) {
	s := openTestStore(t)

	if err := s.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, found, err := s.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Fatal("expected key to be deleted")
	}
}

func TestGrowsAcrossMultiplePages(t *testing.T) {
	s := openTestStore(t)

	// Each value is big enough that only a handful fit per 4KB page,
	// forcing the store to allocate and chain several pages.
	const n = 500
	bigValue := make([]byte, 100)
	for i := range bigValue {
		bigValue[i] = byte(i)
	}

	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		if err := s.Put(key, bigValue); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}

	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		val, found, err := s.Get(key)
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if !found {
			t.Fatalf("key %d not found", i)
		}
		if len(val) != len(bigValue) {
			t.Fatalf("key %d: expected value length %d, got %d", i, len(bigValue), len(val))
		}
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	val, found, err := reopened.Get([]byte("k"))
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if !found || string(val) != "v" {
		t.Fatalf("expected 'v' after reopen, got %q (found=%v)", val, found)
	}
}

func TestWALRecoversOperationBeforePageWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Simulate a crash after the WAL fsync but before the database page write.
	w, err := openWAL(path)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	if err := w.append(walPut, []byte("recovered"), []byte("value")); err != nil {
		t.Fatalf("append WAL: %v", err)
	}
	if err := w.close(); err != nil {
		t.Fatalf("close WAL: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	value, found, err := s.Get([]byte("recovered"))
	if err != nil || !found || string(value) != "value" {
		t.Fatalf("recovered value = %q, found=%v, err=%v", value, found, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close recovered store: %v", err)
	}

	// A clean close checkpoints the WAL, so reopening does not duplicate work.
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	value, found, err = s.Get([]byte("recovered"))
	if err != nil || !found || string(value) != "value" {
		t.Fatalf("reopened value = %q, found=%v, err=%v", value, found, err)
	}
}
