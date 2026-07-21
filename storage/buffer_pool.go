package storage

import (
	"container/list"
	"fmt"
	"sync"
)

type poolEntry struct {
	page  *Page
	dirty bool
}

// BufferPoolStats describes the current cache state and cumulative lookups.
type BufferPoolStats struct {
	Hits        uint64
	Misses      uint64
	CachedPages int
	DirtyPages  int
}

// BufferPool caches a bounded number of pages using LRU eviction. The disk
// callbacks are kept private to Pager so the pool can also be tested in memory.
type BufferPool struct {
	mu        sync.Mutex
	capacity  int
	items     map[uint32]*list.Element
	lru       *list.List
	readDisk  func(uint32) (*Page, error)
	writeDisk func(*Page) error
	hits      uint64
	misses    uint64
}

func NewBufferPool(capacity int, readDisk func(uint32) (*Page, error), writeDisk func(*Page) error) *BufferPool {
	if capacity < 1 {
		capacity = 1
	}
	return &BufferPool{capacity: capacity, items: make(map[uint32]*list.Element), lru: list.New(), readDisk: readDisk, writeDisk: writeDisk}
}

// Get returns a cached page, loading it from disk on a miss.
func (b *BufferPool) Get(id uint32) (*Page, error) {
	b.mu.Lock()
	if e := b.items[id]; e != nil {
		b.hits++
		b.lru.MoveToFront(e)
		page := e.Value.(*poolEntry).page
		b.mu.Unlock()
		return page, nil
	}
	b.mu.Unlock()
	b.mu.Lock()
	b.misses++
	b.mu.Unlock()
	page, err := b.readDisk(id)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.items[id]; e != nil {
		b.lru.MoveToFront(e)
		return e.Value.(*poolEntry).page, nil
	}
	if err := b.makeRoomLocked(); err != nil {
		return nil, err
	}
	b.items[id] = b.lru.PushFront(&poolEntry{page: page})
	return page, nil
}

// Stats returns a snapshot of the buffer pool's counters and current state.
func (b *BufferPool) Stats() BufferPoolStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	dirty := 0
	for _, e := range b.items {
		if e.Value.(*poolEntry).dirty {
			dirty++
		}
	}
	return BufferPoolStats{Hits: b.hits, Misses: b.misses, CachedPages: len(b.items), DirtyPages: dirty}
}

// Put inserts or updates a page. dirty controls whether eviction must persist it.
func (b *BufferPool) Put(page *Page, dirty bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := b.items[page.ID]; e != nil {
		pe := e.Value.(*poolEntry)
		pe.page = page
		pe.dirty = pe.dirty || dirty
		b.lru.MoveToFront(e)
		return nil
	}
	if err := b.makeRoomLocked(); err != nil {
		return err
	}
	b.items[page.ID] = b.lru.PushFront(&poolEntry{page: page, dirty: dirty})
	return nil
}

func (b *BufferPool) makeRoomLocked() error {
	if b.lru.Len() < b.capacity {
		return nil
	}
	e := b.lru.Back()
	pe := e.Value.(*poolEntry)
	if pe.dirty {
		if err := b.writeDisk(pe.page); err != nil {
			return fmt.Errorf("evicting page %d: %w", pe.page.ID, err)
		}
	}
	delete(b.items, pe.page.ID)
	b.lru.Remove(e)
	return nil
}

// Flush persists every dirty page while retaining all cached pages.
func (b *BufferPool) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.items {
		pe := e.Value.(*poolEntry)
		if pe.dirty {
			if err := b.writeDisk(pe.page); err != nil {
				return err
			}
			pe.dirty = false
		}
	}
	return nil
}
