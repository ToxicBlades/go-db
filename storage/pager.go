package storage

import (
	"fmt"
	"os"
)

// Pager owns the on-disk database file and translates page IDs into
// reads/writes at the right byte offset. It deliberately knows nothing
// about what's inside a page (records, keys, indexes) - that's the job
// of the layers built on top of it. Keeping this separation is what
// makes it possible to swap out or extend the storage format later
// without touching the pager at all.
type Pager struct {
	file     *os.File
	numPages uint32
	pool     *BufferPool
}

// Opens (or creates) the database file at path and returns a Pager
// ready to allocate, read, and write pages.
func Open(path string) (*Pager, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening db file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat db file: %w", err)
	}

	if info.Size()%PageSize != 0 {
		f.Close()
		return nil, fmt.Errorf("corrupt db file: size %d is not a multiple of page size %d", info.Size(), PageSize)
	}

	p := &Pager{
		file:     f,
		numPages: uint32(info.Size() / PageSize),
	}
	p.pool = NewBufferPool(64, p.readPageDisk, p.writePageDisk)
	return p, nil
}

// Close flushes and closes the underlying file.
func (p *Pager) Close() error {
	if err := p.Flush(); err != nil {
		return err
	}
	if err := p.file.Sync(); err != nil {
		return err
	}
	return p.file.Close()
}

// AllocatePage appends a brand-new zeroed page to the end of the file
// and returns it. Page IDs start at 0 and increase monotonically -
// there's no reuse of freed pages yet (that's a nice future upgrade
// once you add deletion/compaction).
func (p *Pager) AllocatePage() (*Page, error) {
	id := p.numPages
	page := NewPage(id)

	p.numPages++
	if err := p.pool.Put(page, true); err != nil {
		p.numPages--
		return nil, err
	}
	return page, nil
}

// ReadPage loads the page with the given ID from disk.
func (p *Pager) ReadPage(id uint32) (*Page, error) {
	if id >= p.numPages {
		return nil, fmt.Errorf("page %d does not exist (numPages=%d)", id, p.numPages)
	}

	return p.pool.Get(id)
}

// WritePage writes a page's in-memory contents back to its slot on disk.
func (p *Pager) WritePage(page *Page) error {
	if page.ID >= p.numPages {
		return fmt.Errorf("page %d does not exist (numPages=%d)", page.ID, p.numPages)
	}
	return p.pool.Put(page, true)
}

func (p *Pager) readPageDisk(id uint32) (*Page, error) {
	page := &Page{ID: id}
	offset := int64(id) * PageSize
	if _, err := p.file.ReadAt(page.Data[:], offset); err != nil {
		return nil, fmt.Errorf("reading page %d: %w", id, err)
	}
	return page, nil
}

func (p *Pager) writePageDisk(page *Page) error {
	offset := int64(page.ID) * PageSize
	if _, err := p.file.WriteAt(page.Data[:], offset); err != nil {
		return fmt.Errorf("writing page %d: %w", page.ID, err)
	}
	return nil
}

// Flush writes all dirty cached pages to disk.
func (p *Pager) Flush() error { return p.pool.Flush() }

// Sync forces flushed page contents to stable storage.
func (p *Pager) Sync() error { return p.file.Sync() }

// NumPages returns how many pages currently exist in the file.
func (p *Pager) NumPages() uint32 {
	return p.numPages
}
