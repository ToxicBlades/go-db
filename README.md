# mydb

`mydb` is a small database engine written in Go. It is a learning project with
working storage, indexing, typed tables, SQL, durability, and a TCP interface.

## What is implemented

- Fixed-size 4 KiB pages and a pager backed by a database file (`storage/`).
- An LRU buffer pool with dirty-page flushing (`storage/buffer_pool.go`).
- An append-only key/value store with tombstones, a persisted B+Tree index,
  write-ahead logging, MVCC read snapshots, and explicit compaction (`kv/`).
- Typed rows and schemas supporting `int`, `string`, `bool`, `float`, bytes,
  and timestamps (`kv/table.go`).
- Automatically maintained in-memory secondary indexes for equality lookups on
  non-key table columns (`kv/table.go`).
- A small SQL lexer, parser, and executor supporting `SELECT`, `INSERT`,
  `WHERE`, nested-loop inner joins, batched transactional requests,
  `ORDER BY`, `LIMIT`/`OFFSET`, grouped aggregates, explicit transactions
  (`BEGIN`/`COMMIT`/`ROLLBACK`), `EXPLAIN`, and
  `SHOW TABLES`/`LIST TABLES` (`sql/`).
- Backup and restore commands that snapshot the database file and its WAL and index sidecars (`kv/`, `cmd/mydb/`).
- A line-oriented TCP server accepting plain SQL or JSON requests (`server/`).
- An interactive SQL client (`cmd/mydb/`).

## Architecture

```mermaid
flowchart TD
    CLI[cmd/mydb\ninteractive SQL client] --> TCP[server\nline-oriented TCP]
    TCP --> SQL[sql\nlexer, parser, executor]
    SQL --> TABLE[kv.Table\ntyped rows and schemas]
    TABLE --> STORE[kv.Store\nkey/value records]
    STORE --> INDEX[B+Tree\npersisted index]
    STORE --> WAL[WAL\ncrash recovery]
    STORE --> POOL[BufferPool\nLRU dirty pages]
    POOL --> PAGER[Pager\n4 KiB pages]
    PAGER --> FILE[(database file)]
```

## Request flow

```mermaid
sequenceDiagram
    participant C as SQL client
    participant S as TCP server
    participant E as SQL executor
    participant T as kv.Table
    participant D as Store/WAL
    C->>S: SQL line or JSON request
    S->>E: Execute(query) [connection-local transaction state]
    E->>T: read or write typed row
    T->>D: encode/decode and persist
    D-->>T: result
    T-->>E: rows or affected count
    E-->>S: Result
    S-->>C: JSON response
```

The server serializes access to the shared database. A client with an open
explicit transaction holds this database-level lock until it commits, rolls
back, or disconnects.

## On-disk format

Each page is 4 KiB. Its header stores the page ID, the next-page pointer, and
the offset where the next record can be appended. Records contain a live or
tombstone flag, key/value lengths, and the key/value bytes. Updates append a
new record; reads use the rebuilt B+Tree to find the latest live value. Each
record also receives a sequence number in the in-memory version chain, so a
snapshot can read the value visible at that point with `GetAt` or `ScanAt`.
`Store.Compact` rewrites only live indexed records into a fresh page chain and
replaces the database file, reclaiming overwritten and deleted data.

The WAL is stored beside the database file. Writes are logged and synced before
the corresponding page changes. On startup, complete WAL records are replayed;
a clean close checkpoints the log.

## Run it

```bash
go build ./...
go test ./...

# Start the SQL server (uses seed.sql when present)
make start

# In another terminal, connect with the SQL client
make sql
```

The server can also be started directly. Authentication is enabled by providing
both `--user` and `--password`; clients must provide the same credentials:

```bash
go run ./cmd/mydb server --db mydb.db --addr :5433 --seed seed.sql
go run ./cmd/mydb server --db mydb.db --addr :5433 --user alice --password secret
go run ./cmd/mydb sql --addr :5433 --user alice --password secret

# Snapshot or restore a database and its sibling .wal file
go run ./cmd/mydb backup mydb.db mydb.backup
go run ./cmd/mydb restore mydb.backup mydb.db
```

The SQL client prints result sets as aligned tables:

```text
+----+-------+--------+
| id | name  | active |
+----+-------+--------+
| 1  | Alice | true   |
+----+-------+--------+
```

## Project layout

```text
mydb/
├── cmd/mydb/main.go       # SQL client, server, and backup/restore commands
├── kv/
│   ├── btree.go           # B+Tree index and persistence
│   ├── store.go           # append-only records and store operations
│   ├── table.go           # typed schemas and rows
│   └── wal.go             # write-ahead log and recovery
├── server/server.go       # newline-delimited TCP/JSON protocol
├── sql/sql.go             # SQL lexer, parser, and executor
├── storage/
│   ├── buffer_pool.go     # LRU page cache
│   ├── page.go            # page representation and layout
│   └── pager.go           # database-file page I/O
├── seed.sql               # optional startup seed data
├── Makefile
└── README.md
```

## References

- *Database Internals* by Alex Petrov
- [CMU Intro to Database Systems](https://www.youtube.com/@cmudatabasegroup)
- [BoltDB](https://github.com/etcd-io/bbolt) and [Badger](https://github.com/dgraph-io/badger)
- [Let's Build a Simple Database](https://cstack.github.io/db_tutorial/)
