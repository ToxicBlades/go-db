package kv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndRestoreIncludesSidecars(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	backup := filepath.Join(dir, "backup.db")
	restored := filepath.Join(dir, "restored.db")

	s, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put([]byte("key"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source+".catalog", []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Backup(source, backup); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range backupSuffixes {
		if _, err := os.Stat(backup + suffix); err != nil {
			t.Fatalf("missing backup sidecar %q: %v", suffix, err)
		}
	}
	if err := Restore(backup, restored); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	value, found, err := reopened.Get([]byte("key"))
	if err != nil || !found || string(value) != "value" {
		t.Fatalf("restored value=%q found=%v err=%v", value, found, err)
	}
}

func TestBackupRejectsCorruptDatabase(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	s, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put([]byte("key"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(source, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the page ID while leaving the file size valid.
	if _, err := file.WriteAt([]byte{9}, 0); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Backup(source, filepath.Join(dir, "backup.db")); err == nil {
		t.Fatal("expected corrupt database to be rejected")
	}
}

func TestRestoreRejectsCorruptWALWithoutReplacingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.db")
	destination := filepath.Join(dir, "destination.db")
	for _, path := range []string{source, destination} {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Put([]byte("key"), []byte(path)); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	corruptWAL := make([]byte, walHeader)
	copy(corruptWAL, "corrupt-wal")
	if err := os.WriteFile(source+".wal", corruptWAL, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restore(source, destination); err == nil {
		t.Fatal("expected corrupt WAL to be rejected")
	}
	s, err := Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	value, _, err := s.Get([]byte("key"))
	if err != nil || string(value) != destination {
		t.Fatalf("destination changed after failed restore: %q, %v", value, err)
	}
}
