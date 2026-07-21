## SQL / query engine
- **`ORDER BY` / `LIMIT` / `OFFSET`**
- **Aggregates**: `COUNT`, `SUM`, `AVG`, `MIN`, `MAX`, with `GROUP BY`.
- **Joins** — even just a naive nested-loop inner join would be a big milestone given your single-table executor today.
- **Multiple statements per request** (currently rejected) — batch execution with transaction semantics.

## Transactions / concurrency
- **Explicit transactions** (`BEGIN`/`COMMIT`/`ROLLBACK`) — your WAL groundwork makes this a natural fit.
- **Isolation / locking** — right now it sounds single-threaded per request; consider row-level or table-level locks if you want concurrent clients to write safely.
- **MVCC** — more ambitious, but a great learning exercise given you already have append-only records with a "latest live value" model.

## Storage engine
- **On-disk B+Tree instead of in-memory** — currently the index is rebuilt/held in memory (`kv/btree.go`); persisting it would remove the rebuild-on-startup cost and is a classic next step after WAL.
- **Compaction / vacuum** — your append-only + tombstone model will grow forever without a compaction pass to reclaim space.
- **Secondary indexes** — right now it looks like the key column is the only indexed path; a secondary index structure would let `WHERE` on non-key columns be fast instead of a full scan.

## Types / schema
- **More types**: `float`/`double`, `bytes`/`blob`, `timestamp`.

## Server / client
- **Prepared statements / parameterized queries** — avoids re-parsing and is safer than string-building SQL.
- **Authentication** on the TCP server.
- **`EXPLAIN`** — show the query plan, even a trivial one; good for understanding your own executor's behavior.

## Tooling / ops
- **Metrics/stats** — buffer pool hit rate, page count, WAL size — since you already track LRU/dirty pages, exposing them is cheap and satisfying.
- **Backup/restore command** — snapshot the db file + WAL.
- **Fuzz testing** on the SQL parser and WAL recovery path — recovery code is exactly the kind of thing that hides bugs until a real crash.
