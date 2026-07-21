package kv

import (
	"fmt"
	"io"
	"os"
)

// Backup copies a database and its WAL sidecar to destination and
// destination.wal. The source should not be open for writes while backing up.
func Backup(source, destination string) error {
	if source == destination {
		return fmt.Errorf("backup destination must differ from source")
	}
	if err := copyFile(source, destination); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}
	if err := copyFile(source+".wal", destination+".wal"); err != nil {
		return fmt.Errorf("copy WAL: %w", err)
	}
	if err := copyFile(source+".idx", destination+".idx"); err != nil {
		return fmt.Errorf("copy index: %w", err)
	}
	return nil
}

// Restore replaces destination and its WAL sidecar with a backup pair.
func Restore(source, destination string) error {
	if source == destination {
		return fmt.Errorf("restore destination must differ from source")
	}
	if err := copyFile(source, destination); err != nil {
		return fmt.Errorf("restore database: %w", err)
	}
	if err := copyFile(source+".wal", destination+".wal"); err != nil {
		return fmt.Errorf("restore WAL: %w", err)
	}
	if err := copyFile(source+".idx", destination+".idx"); err != nil {
		return fmt.Errorf("restore index: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
