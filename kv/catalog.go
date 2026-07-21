package kv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CatalogEntry describes a durable table registration.
type CatalogEntry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Schema Schema `json:"schema"`
}

// Catalog stores table metadata beside the database file. Table data remains
// in its own Store so existing store/WAL recovery can be reused unchanged.
type Catalog struct {
	path    string
	entries map[string]CatalogEntry
}

// OpenCatalog opens or creates a catalog at path.
func OpenCatalog(path string) (*Catalog, error) {
	c := &Catalog{path: path, entries: map[string]CatalogEntry{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	var entries []CatalogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	for _, entry := range entries {
		if entry.Name == "" || entry.Path == "" {
			return nil, fmt.Errorf("invalid catalog entry")
		}
		if _, exists := c.entries[entry.Name]; exists {
			return nil, fmt.Errorf("duplicate catalog table %q", entry.Name)
		}
		if err := entry.Schema.validate(); err != nil {
			return nil, fmt.Errorf("catalog table %q: %w", entry.Name, err)
		}
		c.entries[entry.Name] = entry
	}
	return c, nil
}

// Entries returns the catalog entries in stable name order.
func (c *Catalog) Entries() []CatalogEntry {
	entries := make([]CatalogEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Set registers or replaces a table and persists the catalog atomically.
func (c *Catalog) Set(entry CatalogEntry) error {
	if entry.Name == "" || entry.Path == "" {
		return fmt.Errorf("catalog table name and path are required")
	}
	if err := entry.Schema.validate(); err != nil {
		return err
	}
	c.entries[entry.Name] = entry
	return c.save()
}

// Delete removes a table registration and persists the catalog atomically.
func (c *Catalog) Delete(name string) error {
	delete(c.entries, name)
	return c.save()
}

func (c *Catalog) save() error {
	b, err := json.MarshalIndent(c.Entries(), "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".catalog-*")
	if err != nil {
		return fmt.Errorf("create catalog temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write catalog: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync catalog: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, c.path); err != nil {
		return fmt.Errorf("replace catalog: %w", err)
	}
	return nil
}
