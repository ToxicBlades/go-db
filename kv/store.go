// Package kv implements a disk-backed key-value store. Records are appended
// to fixed-size pages, and a persisted B+Tree provides indexed lookups.
package kv

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"os"

	"mydb/storage"
)

const (
	flagLive      byte = 0
	flagTombstone byte = 1

	// recordHeaderSize: 1 byte flag + 2 bytes key length + 2 bytes value length.
	recordHeaderSize = 5
)

// Store is a key-value store built directly on top of the Pager.
type Store struct {
	path      string
	pager     *storage.Pager
	firstPage uint32
	hasPages  bool
	index     btree
	wal       *wal
	indexPath string
}

// Stats is a point-in-time snapshot of storage metrics for this store.
type Stats struct {
	BufferPool storage.BufferPoolStats
	PageCount  uint32
	WALSize    int64
}

// Stats returns buffer pool, page count, and WAL size metrics.
func (s *Store) Stats() (Stats, error) {
	walSize, err := s.wal.size()
	if err != nil {
		return Stats{}, err
	}
	return Stats{BufferPool: s.pager.BufferPoolStats(), PageCount: s.pager.NumPages(), WALSize: walSize}, nil
}

// Open opens a Store backed by the file at path, creating it if needed.
func Open(path string) (*Store, error) {
	pager, err := storage.Open(path)
	if err != nil {
		return nil, err
	}

	w, err := openWAL(path)
	if err != nil {
		pager.Close()
		return nil, err
	}
	s := &Store{path: path, pager: pager, wal: w, indexPath: path + ".idx"}
	indexLoaded, err := s.loadIndex()
	if err != nil {
		w.close()
		pager.Close()
		return nil, fmt.Errorf("load index: %w", err)
	}
	if err := w.replay(func(op byte, key, value []byte) error {
		return s.append(key, value, map[byte]byte{walPut: flagLive, walDelete: flagTombstone}[op])
	}); err != nil {
		w.close()
		pager.Close()
		return nil, fmt.Errorf("WAL recovery: %w", err)
	}
	if pager.NumPages() > 0 && !indexLoaded {
		s.firstPage = 0
		s.hasPages = true
		if err := s.rebuildIndex(); err != nil {
			pager.Close()
			return nil, err
		}
	}
	if !indexLoaded {
		if err := s.saveIndex(); err != nil {
			w.close()
			pager.Close()
			return nil, err
		}
	}
	return s, nil
}

// Close flushes and closes the underlying file.
func (s *Store) Close() error {
	if err := s.pager.Flush(); err != nil {
		return err
	}
	if err := s.pager.Sync(); err != nil {
		return err
	}
	if err := s.saveIndex(); err != nil {
		return err
	}
	if err := s.wal.clear(); err != nil {
		return err
	}
	if err := s.wal.close(); err != nil {
		return err
	}
	return s.pager.Close()
}

// record is one key/value entry as read off a page.
type record struct {
	flag  byte
	key   []byte
	value []byte
	size  int // total bytes this record occupies, header included
}

func readRecord(data []byte, offset uint16) (*record, error) {
	if int(offset)+recordHeaderSize > len(data) {
		return nil, fmt.Errorf("record header out of bounds at offset %d", offset)
	}
	flag := data[offset]
	keyLen := binary.LittleEndian.Uint16(data[offset+1 : offset+3])
	valLen := binary.LittleEndian.Uint16(data[offset+3 : offset+5])

	start := int(offset) + recordHeaderSize
	end := start + int(keyLen) + int(valLen)
	if end > len(data) {
		return nil, fmt.Errorf("record body out of bounds at offset %d", offset)
	}

	return &record{
		flag:  flag,
		key:   data[start : start+int(keyLen)],
		value: data[start+int(keyLen) : end],
		size:  recordHeaderSize + int(keyLen) + int(valLen),
	}, nil
}

// writeRecord appends a record to the page's free space. Returns an
// error if the page doesn't have enough room - the caller is expected
// to allocate a new page and try again.
func writeRecord(page *storage.Page, key, value []byte, flag byte) error {
	needed := recordHeaderSize + len(key) + len(value)
	if page.Remaining() < needed {
		return fmt.Errorf("page full")
	}

	offset := page.FreeOffset()
	data := page.Data[:]

	data[offset] = flag
	binary.LittleEndian.PutUint16(data[offset+1:offset+3], uint16(len(key)))
	binary.LittleEndian.PutUint16(data[offset+3:offset+5], uint16(len(value)))

	start := int(offset) + recordHeaderSize
	copy(data[start:], key)
	copy(data[start+len(key):], value)

	page.SetFreeOffset(offset + uint16(needed))
	return nil
}

// Get returns the value for key, or found=false if it doesn't exist
// (or was deleted). Because writes are append-only, if a key was
// written more than once, the last write in scan order wins - that's
// why we keep scanning to the end instead of stopping at the first match.
func (s *Store) Get(key []byte) (value []byte, found bool, err error) {
	value, found = s.index.get(key)
	return append([]byte(nil), value...), found, nil
}

// Put writes (or overwrites) the value for key.
func (s *Store) Put(key, value []byte) error {
	if err := s.wal.append(walPut, key, value); err != nil {
		return err
	}
	return s.append(key, value, flagLive)
}

// Delete marks key as removed by appending a tombstone record.
// The old record's bytes stay on disk until a future compaction pass
// reclaims them - that's a deliberate simplification for milestone 1.
func (s *Store) Delete(key []byte) error {
	if err := s.wal.append(walDelete, key, nil); err != nil {
		return err
	}
	if err := s.append(key, nil, flagTombstone); err != nil {
		return err
	}
	s.index.delete(key)
	return s.saveIndex()
}

// Compact rewrites the store with one record for each live key, reclaiming
// space occupied by overwritten values and tombstones.
func (s *Store) Compact() error {
	tmp := s.path + ".compact.tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	newPager, err := storage.Open(tmp)
	if err != nil {
		return err
	}
	var first, last *storage.Page
	for _, entry := range s.index.entries() {
		if last == nil {
			last, err = newPager.AllocatePage()
			if err != nil {
				break
			}
			first = last
		}
		if err = writeRecord(last, entry.Key, entry.Value, flagLive); err != nil {
			previous := last
			last, err = newPager.AllocatePage()
			if err == nil {
				previous.SetNextPageID(last.ID)
				err = newPager.WritePage(previous)
			}
			if err == nil {
				err = writeRecord(last, entry.Key, entry.Value, flagLive)
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		_ = newPager.Close()
		_ = os.Remove(tmp)
		return err
	}
	if last != nil {
		err = newPager.WritePage(last)
	}
	if err == nil {
		err = newPager.Flush()
	}
	if err == nil {
		err = newPager.Sync()
	}
	if closeErr := newPager.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = s.pager.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		s.pager, _ = storage.Open(s.path)
		return err
	}
	s.pager, err = storage.Open(s.path)
	if err != nil {
		return err
	}
	s.firstPage = 0
	s.hasPages = first != nil
	return s.wal.clear()
}

// append writes a record to the last page in the chain, allocating a
// new page if there isn't enough room.
func (s *Store) append(key, value []byte, flag byte) error {
	lastPage, err := s.lastPage()
	if err != nil {
		return err
	}

	if err := writeRecord(lastPage, key, value, flag); err != nil {
		// Current page is full - allocate a new one and link it in.
		newPage, allocErr := s.pager.AllocatePage()
		if allocErr != nil {
			return allocErr
		}
		if writeErr := writeRecord(newPage, key, value, flag); writeErr != nil {
			return fmt.Errorf("record too large for an empty page: %w", writeErr)
		}
		lastPage.SetNextPageID(newPage.ID)
		if err := s.pager.WritePage(lastPage); err != nil {
			return err
		}
		if err := s.pager.WritePage(newPage); err != nil {
			return err
		}
		if flag == flagLive {
			s.index.put(key, value)
		} else {
			s.index.delete(key)
		}
		if err := s.saveIndex(); err != nil {
			return err
		}
		return nil
	}

	if err := s.pager.WritePage(lastPage); err != nil {
		return err
	}
	if flag == flagLive {
		s.index.put(key, value)
	}
	if err := s.saveIndex(); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadIndex() (bool, error) {
	f, err := os.Open(s.indexPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	var entries []btreeEntry
	if err := gob.NewDecoder(f).Decode(&entries); err != nil {
		return false, err
	}
	for _, e := range entries {
		s.index.put(e.Key, e.Value)
	}
	return true, nil
}

func (s *Store) saveIndex() error {
	data := new(bytes.Buffer)
	if err := gob.NewEncoder(data).Encode(s.index.entries()); err != nil {
		return err
	}
	tmp := s.indexPath + ".tmp"
	if err := os.WriteFile(tmp, data.Bytes(), 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}
	if err := os.Rename(tmp, s.indexPath); err != nil {
		return fmt.Errorf("replace index: %w", err)
	}
	return nil
}

func (s *Store) rebuildIndex() error {
	for id := s.firstPage; ; {
		p, err := s.pager.ReadPage(id)
		if err != nil {
			return err
		}
		for off := uint16(storage.HeaderSize); off < p.FreeOffset(); {
			r, err := readRecord(p.Data[:], off)
			if err != nil {
				return err
			}
			if r.flag == flagTombstone {
				s.index.delete(r.key)
			} else {
				s.index.put(r.key, r.value)
			}
			off += uint16(r.size)
		}
		if p.NextPageID() == storage.NoPage {
			return nil
		}
		id = p.NextPageID()
	}
}

// lastPage returns the last page in the chain, allocating the very
// first page if the store is currently empty.
func (s *Store) lastPage() (*storage.Page, error) {
	if !s.hasPages {
		page, err := s.pager.AllocatePage()
		if err != nil {
			return nil, err
		}
		s.firstPage = page.ID
		s.hasPages = true
		return page, nil
	}

	page, err := s.pager.ReadPage(s.firstPage)
	if err != nil {
		return nil, err
	}
	for page.NextPageID() != storage.NoPage {
		page, err = s.pager.ReadPage(page.NextPageID())
		if err != nil {
			return nil, err
		}
	}
	return page, nil
}
