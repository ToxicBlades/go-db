package storage

import (
	"path/filepath"
	"testing"
)

func TestAllocateAndReadPage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	pager, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pager.Close()

	page, err := pager.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage: %v", err)
	}
	if page.ID != 0 {
		t.Fatalf("expected first page ID 0, got %d", page.ID)
	}
	if page.NextPageID() != NoPage {
		t.Fatalf("expected new page to have no next page, got %d", page.NextPageID())
	}

	// Write some data into the page and persist it.
	copy(page.Data[HeaderSize:], []byte("hello"))
	page.SetFreeOffset(HeaderSize + 5)
	if err := pager.WritePage(page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// Read it back and check the contents survived the round trip.
	got, err := pager.ReadPage(0)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if string(got.Data[HeaderSize:HeaderSize+5]) != "hello" {
		t.Fatalf("expected 'hello', got %q", got.Data[HeaderSize:HeaderSize+5])
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	pager, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	page, err := pager.AllocatePage()
	if err != nil {
		t.Fatalf("AllocatePage: %v", err)
	}
	copy(page.Data[HeaderSize:], []byte("persisted"))
	if err := pager.WritePage(page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if err := pager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the same file and make sure the page is still there.
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	if reopened.NumPages() != 1 {
		t.Fatalf("expected 1 page after reopen, got %d", reopened.NumPages())
	}

	got, err := reopened.ReadPage(0)
	if err != nil {
		t.Fatalf("ReadPage after reopen: %v", err)
	}
	if string(got.Data[HeaderSize:HeaderSize+9]) != "persisted" {
		t.Fatalf("expected 'persisted', got %q", got.Data[HeaderSize:HeaderSize+9])
	}
}

func TestReadPageOutOfBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	pager, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pager.Close()

	if _, err := pager.ReadPage(0); err == nil {
		t.Fatal("expected error reading nonexistent page 0, got nil")
	}
}
