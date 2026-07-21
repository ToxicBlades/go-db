package storage

import "encoding/binary"

const (
	// PageSize is the fixed size, in bytes, of every page on disk.
	// Real databases usually match this to the OS/filesystem block
	// size (4KB is the classic choice) so a page read/write maps
	// cleanly onto one disk I/O operation.
	PageSize = 4096

	// HeaderSize is how many bytes at the start of every page are
	// reserved for page metadata (id, free offset, next-page pointer).
	// Everything after this is available for records.
	HeaderSize = 16
)

// NoPage is the sentinel value meaning "there is no next page in this
// chain." We can't use 0 for this because 0 is a valid page ID.
const NoPage uint32 = ^uint32(0)

// Page represents a single fixed-size page of raw bytes, exactly as it
// sits on disk. Everything above this layer (records, indexes, rows)
// is just an interpretation of these bytes - the page itself doesn't
// know or care what's stored in it.
//
// Layout:
//
//	[0:4]   page ID
//	[4:6]   free offset (where the next record can be written)
//	[6:10]  next page ID (NoPage if this is the last page in its chain)
//	[10:16] reserved for future use (e.g. checksums)
//	[16:..] record data
type Page struct {
	ID   uint32
	Data [PageSize]byte
}

// NewPage creates a zeroed page with the given ID and an initialized
// header: no records yet, and no next page in the chain.
func NewPage(id uint32) *Page {
	p := &Page{ID: id}
	binary.LittleEndian.PutUint32(p.Data[0:4], id)
	binary.LittleEndian.PutUint16(p.Data[4:6], HeaderSize)
	binary.LittleEndian.PutUint32(p.Data[6:10], NoPage)
	return p
}

// FreeOffset returns the byte offset where the next record can be written.
func (p *Page) FreeOffset() uint16 {
	return binary.LittleEndian.Uint16(p.Data[4:6])
}

// SetFreeOffset updates where the next record write should begin.
func (p *Page) SetFreeOffset(off uint16) {
	binary.LittleEndian.PutUint16(p.Data[4:6], off)
}

// NextPageID returns the ID of the next page in this page's chain,
// or NoPage if this is the last page.
func (p *Page) NextPageID() uint32 {
	return binary.LittleEndian.Uint32(p.Data[6:10])
}

// SetNextPageID links this page to the next one in its chain.
func (p *Page) SetNextPageID(id uint32) {
	binary.LittleEndian.PutUint32(p.Data[6:10], id)
}

// Remaining returns how many free bytes are left in the page for new records.
func (p *Page) Remaining() int {
	return PageSize - int(p.FreeOffset())
}
