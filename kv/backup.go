package kv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go-db/storage"
)

var backupSuffixes = []string{"", ".wal", ".idx", ".catalog"}

// Backup creates a consistent copy of a closed database and all of its
// sidecars. The source must not be open for writes while the backup runs.
func Backup(source, destination string) error {
	if source == destination {
		return fmt.Errorf("backup destination must differ from source")
	}
	if err := validateBackup(source); err != nil {
		return fmt.Errorf("validate backup source: %w", err)
	}
	return copyBundle(source, destination, "backup")
}

// Restore validates a backup before replacing the destination and its
// sidecars. The destination is unchanged if validation or staging fails.
func Restore(source, destination string) error {
	if source == destination {
		return fmt.Errorf("restore destination must differ from source")
	}
	if err := validateBackup(source); err != nil {
		return fmt.Errorf("validate backup: %w", err)
	}
	return copyBundle(source, destination, "restore")
}

func validateBackup(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size()%storage.PageSize != 0 {
		return fmt.Errorf("database size %d is not a multiple of page size %d", info.Size(), storage.PageSize)
	}
	// Opening validates page structure, the persisted index, and WAL checksums.
	// A closed source is required because recovery may replay its WAL.
	s, err := Open(path)
	if err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(path + ".idx"); err != nil {
		return fmt.Errorf("index sidecar: %w", err)
	}
	if _, err := os.Stat(path + ".catalog"); err == nil {
		if _, err := OpenCatalog(path + ".catalog"); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func copyBundle(source, destination, operation string) error {
	type stagedFile struct{ temp, target string }
	staged := make([]stagedFile, 0, len(backupSuffixes))
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
		}
	}()
	for _, suffix := range backupSuffixes {
		src := source + suffix
		target := destination + suffix
		if _, err := os.Stat(src); err != nil {
			if suffix == ".catalog" && os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("%s %s: %w", operation, suffix, err)
		}
		tmp, err := os.CreateTemp(filepath.Dir(target), ".go-db-backup-*")
		if err != nil {
			return fmt.Errorf("stage %s: %w", suffix, err)
		}
		temp := tmp.Name()
		if err := copyInto(tmp, src); err != nil {
			tmp.Close()
			os.Remove(temp)
			return fmt.Errorf("stage %s: %w", suffix, err)
		}
		if err := tmp.Close(); err != nil {
			os.Remove(temp)
			return err
		}
		staged = append(staged, stagedFile{temp: temp, target: target})
	}
	for _, file := range staged {
		if err := os.Rename(file.temp, file.target); err != nil {
			return fmt.Errorf("replace %s: %w", file.target, err)
		}
	}
	return nil
}

func copyInto(out *os.File, source string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
