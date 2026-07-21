# mydb

A database engine, built from scratch in Go, one layer at a time.

This is a learning/portfolio project: the goal is to understand (and be
able to explain) how real databases work under the hood — pages, buffer
pools, B-trees, write-ahead logging — by building a small but *real*
version of each piece.

## Status: Milestone 6 — write-ahead logging

What's implemented so far:

- **Storage layer** (`storage/`): fixed-size 4KB pages, read/written
  directly to a single file via `os.File.ReadAt`/`WriteAt`. No OS-level
  caching assumptions — every read/write is an explicit page-sized I/O.
- **Key-value store** (`kv/`): an append-only, linear-scan store built
  on top of the page layer. Records are packed into pages; when a page
  fills up, a new one is allocated and linked via a `nextPageID` pointer
  in the page header, forming a chain.
- **CLI** (`cmd/mydb/`): a tiny interactive shell to `put`/`get`/`delete`
  keys against a real file on disk.
- **Rows and schemas** (`kv/table.go`): typed `int`, `string`, and `bool`
  columns with a `Table` abstraction over the store.
- **Tiny SQL layer** (`sql/`): hand-written lexer/parser and naive executor
  for `SELECT`, `INSERT`, and equality `WHERE` queries.
- **Write-ahead log** (`kv/`): Put/Delete operations are fsynced to a sidecar
  WAL before page changes; startup replays complete records, and clean close
  checkpoints the log.

### On-disk format

**Page header (16 bytes):**

| Bytes | Field       | Meaning                                  |
|-------|-------------|-------------------------------------------|
| 0–4   | page ID     | this page's own ID                        |
| 4–6   | free offset | where the next record write should start  |
| 6–10  | next page   | ID of the next page in the chain (or `NoPage` sentinel) |
| 10–16 | reserved    | unused for now (future: checksums)        |

**Record format** (packed one after another starting right after the header):

| Bytes | Field    |
|-------|----------|
| 1     | flag (`live` / `tombstone`) |
| 2     | key length  |
| 2     | value length |
| N     | key bytes   |
| M     | value bytes |

Writes are **append-only**: `Put` always appends a new record rather
than mutating one in place, and `Delete` appends a tombstone record.
`Get` scans every record for a matching key and keeps the *last* one it
sees, so later writes correctly shadow earlier ones. This is simple and
correct, but O(n) per lookup — which is exactly the problem the next
milestone exists to fix.

## Why it's built this way

The point of doing this linear-scan version first, instead of jumping
straight to a B-tree, is that it forces the on-disk record format and
page layout to be correct and tested *before* adding the complexity of
an index on top. Every later milestone reuses this same page/record
format — the B-tree just changes *how you find* a record, not what a
record looks like on disk.

## Running it

```bash
# Build
go build ./...

# Run the tests
go test ./...

# Play with it interactively
go run ./cmd/mydb mydb.db
> put name alice
OK
> get name
alice
> delete name
OK
> get name
(not found)
> exit
```

## Roadmap

- [x] **1. Storage layer** — pages, disk I/O
- [x] **1. Key-value store** — append-only records, page chaining
- [x] **2. Buffer pool** — cache hot pages in memory (LRU/clock eviction),
      track dirty pages, only hit disk on eviction or explicit flush
- [x] **3. B+Tree index** — replace the linear scan with O(log n)
      lookups; implement node splits on insert and merges on delete
- [x] **4. Rows & schema** — typed columns (int, string, bool), a
      `Table` abstraction instead of raw key/value
- [x] **5. Tiny SQL layer** — hand-written lexer/parser for a subset of
      SQL (`SELECT`, `INSERT`, `WHERE`), plus a naive query executor
- [x] **6. Write-ahead log** — durability and crash recovery: log
      operations before applying them, replay on restart
- [ ] **7. Network protocol** (stretch) — a TCP server so it can be
      queried like a real database, not just via the CLI

## Project layout

```
mydb/
├── storage/
│   ├── page.go        # page layout: header fields, free offset, chaining
│   ├── pager.go        # reads/writes fixed-size pages to the db file
│   └── pager_test.go
├── kv/
│   ├── store.go         # append-only record format + linear-scan Put/Get/Delete
│   └── store_test.go
├── cmd/mydb/
│   └── main.go           # interactive CLI
└── README.md
```

## References this project is following

- *Database Internals* by Alex Petrov
- CMU's [Intro to Database Systems](https://www.youtube.com/@cmudatabasegroup)
- [BoltDB](https://github.com/etcd-io/bbolt) / [Badger](https://github.com/dgraph-io/badger)
- ["Let's Build a Simple Database"](https://cstack.github.io/db_tutorial/)
