# TODO

## Current state

`go-db` is a compact but surprisingly complete learning database. The core
path works end to end: fixed-size pages and an LRU buffer pool feed an
append-only store; the store has a persisted B+Tree, WAL recovery, snapshots,
and compaction; typed tables add schema validation and in-memory secondary
indexes; SQL supports CRUD, joins, aggregates, transactions, prepared
statements, `EXPLAIN`, and table management; and a line-oriented TCP server and
CLI expose it.

The implementation is well covered for the main happy paths, persistence,
transaction snapshots/conflicts, parser fuzzing, WAL fuzzing, and the basic
network protocol. It is still primarily a single-process educational engine:
the server supplies a hard-coded `users` table, while tables and secondary
indexes are not reconstructed from durable catalog metadata on restart.

## Suggested work

### Highest value

- [ ] Add a durable system catalog for tables, schemas, column constraints,
  and table names. Reopen should restore `CREATE TABLE`/`ALTER TABLE` state
  instead of requiring the server to recreate `users` in code.
- [ ] Persist or rebuild secondary indexes on table open. Define an index
  version/metadata format and make index creation, updates, deletes, and schema
  changes crash-safe.
- [ ] Add end-to-end restart tests: create/alter/drop tables, insert/update/
  delete rows, reopen the database, and verify schemas, constraints, rows, and
  indexes all survive.
- [ ] Make backup consistent and safer: coordinate with an open store, flush
  and sync before copying, copy to temporary files, then atomically rename;
  restore should validate all required sidecars before replacing anything.
- [ ] Add corruption detection and recovery diagnostics (page/WAL checksums,
  length validation, truncated-record handling, and clearer repair errors).

### SQL and query execution

- [ ] Add negative numeric literals, `IS NULL`/`IS NOT NULL`, and explicit
  NULL semantics for comparisons and boolean expressions.
- [ ] Add `UPDATE ... FROM`/more join forms only if needed; otherwise document
  the intentionally small SQL surface and test unsupported syntax clearly.
- [ ] Replace the nested-loop join with a hash join or an indexed join when a
  suitable equality key exists; include plan choices in `EXPLAIN`.
- [ ] Add a real query-planning layer: use secondary indexes for equality
  predicates in SQL, estimate/select scan strategies, and avoid sorting or
  grouping more rows than necessary.
- [ ] Add tests for duplicate constraints, foreign-key enforcement, NULLs,
  aggregates over empty input, ordering ties, pagination boundaries, and
  atomic multi-statement failures.

### Transactions and concurrency

- [x] Put transaction state and table/catalog changes behind explicit
  synchronization. Add concurrent reader/writer and concurrent commit tests
  under `go test -race`.
- [ ] Decide and document the isolation/locking model, especially write-write
  conflicts, phantoms, DDL during transactions, and whether a transaction can
  span clients or only one connection.
- [ ] Ensure all transaction paths release snapshots and roll back cleanly on
  connection errors, commit failures, and process shutdown.

### Storage and operations

- [ ] Add a background checkpoint/compaction policy and expose an explicit
  administrative command or API for stats, flush, and compaction.
- [ ] Improve buffer-pool behavior under contention (avoid duplicate reads and
  make dirty-page ownership/eviction guarantees explicit).
- [ ] Add page free-space management and page reuse after compaction, instead
  of only growing the file monotonically.
- [ ] Add configurable page size, buffer-pool capacity, and server settings
  only where they are useful; keep defaults simple.
- [ ] Add protocol limits and timeouts for oversized lines, idle clients, and
  slow readers, plus graceful shutdown tests.
- [ ] Hash passwords instead of retaining a plaintext server password, and
  document that the current authentication is not encrypted without TLS.

### Developer experience

- [ ] Add integration tests that start the real server and exercise the CLI/
  JSON protocol against a temporary database.
- [ ] Add benchmarks for point lookups, scans, indexed predicates, joins,
  compaction, and WAL recovery.
- [ ] Add invariant checks for B+Tree ordering, index consistency, page links,
  and store sequence/version chains; run them in tests and optionally via a
  debug command.
- [ ] Add a small example showing the Go API for opening a store, creating a
  table, using a prepared statement, and closing it safely.
- [ ] Keep `README.md` and `SQL_COMMANDS.md` synchronized as durable catalogs,
  SQL behavior, commands, and operational guarantees evolve.

## Recommended order

1. Durable catalog and restart tests.
2. Consistent backup/restore and corruption checks.
3. Transaction/concurrency tests with the race detector.
4. SQL NULL behavior and constraint edge cases.
5. Query planning/index use, then performance work.
