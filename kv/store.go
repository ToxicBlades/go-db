// Package kv implements milestone 1 of the database engine: a working,
// disk-backed key-value store. Records are appended to fixed-size pages,
// and pages are chained together as they fill up. Lookups are a linear
// scan over every record - this is intentionally the "dumb" version.
// Once this is correct and tested, milestone 3 replaces the scan with
// a B+Tree index for real performance, without changing the on-disk
// record format at all.
package kv

import (
	"encoding/binary"
	"fmt"

	"mydb/storage"
)

const (
	flagLive      byte = 0
	flagTombstone byte = 1

	// recordHeaderSize: 1 byte flag + 2 bytes key length + 2 bytes value length.
	recordHeaderSize = 5
)

// Store is a simple, linear-scan key-value store built directly on top
// of the Pager.
type Store struct {
	pager     *storage.Pager
	firstPage uint32
	hasPages  bool
}

// Open opens a Store backed by the file at path, creating it if needed.
func Open(path string) (*Store, error) {
	pager, err := storage.Open(path)
	if err != nil {
		return nil, err
	}

	s := &Store{pager: pager}
	if pager.NumPages() > 0 {
		s.firstPage = 0
		s.hasPages = true
	}
	return s, nil
}

// Close flushes and closes the underlying file.
func (s *Store) Close() error {
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
	if !s.hasPages {
		return nil, false, nil
	}

	pageID := s.firstPage
	for {
		page, err := s.pager.ReadPage(pageID)
		if err != nil {
			return nil, false, err
		}

		offset := uint16(storage.HeaderSize)
		for offset < page.FreeOffset() {
			rec, err := readRecord(page.Data[:], offset)
			if err != nil {
				return nil, false, err
			}
			if string(rec.key) == string(key) {
				if rec.flag == flagTombstone {
					value, found = nil, false
				} else {
					value = append([]byte(nil), rec.value...)
					found = true
				}
			}
			offset += uint16(rec.size)
		}

		next := page.NextPageID()
		if next == storage.NoPage {
			break
		}
		pageID = next
	}

	return value, found, nil
}

// Put writes (or overwrites) the value for key.
func (s *Store) Put(key, value []byte) error {
	return s.append(key, value, flagLive)
}

// Delete marks key as removed by appending a tombstone record.
// The old record's bytes stay on disk until a future compaction pass
// reclaims them - that's a deliberate simplification for milestone 1.
func (s *Store) Delete(key []byte) error {
	return s.append(key, nil, flagTombstone)
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
		return s.pager.WritePage(newPage)
	}

	return s.pager.WritePage(lastPage)
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
