## Transactions / concurrency

### MVCC roadmap

- [x] **Storage snapshots** — assign append sequence numbers, retain per-key
  version chains, and expose `Store.BeginSnapshot`, `GetAt`, and `KeysAt`.
- [x] **Table snapshots** — expose `Table.Snapshot`, `GetAt`, and `ScanAt` for
  consistent row reads.
- [x] **Transaction-owned state** — replace SQL's snapshot/restore transaction
  implementation with a transaction object containing a read timestamp,
  pending writes, and transaction status.
- [x] **Snapshot-consistent SQL reads** — make `SELECT`, `UPDATE`, `DELETE`,
  uniqueness checks, foreign-key checks, and generated IDs read through the
  transaction snapshot.
- [x] **Read-your-writes** — overlay a transaction's uncommitted inserts,
  updates, and deletes on snapshot reads without exposing them to other
  transactions.
- [x] **Atomic commit** — stage a transaction's writes and apply them at
  `COMMIT`; make them visible only after commit, and discard staged writes on
  rollback. The underlying WAL batch/commit marker is still future work.
- [x] **Durable transaction metadata** — extend the WAL with transaction IDs,
  commit records, and recovery rules for committed versus incomplete batches.
- [x] **Concurrent transactions** — allow independent client transactions to
  interleave between requests while preserving snapshot reads. Write conflict
  detection remains a separate step.
- [ ] **Conflict detection** — detect write/write conflicts and define the
  behavior for concurrent updates or deletes of the same key.
- [ ] **Commit ordering** — use a single durable commit sequence so snapshots
  have a stable ordering across tables and after restart.
- [ ] **MVCC-aware compaction** — retain versions needed by active snapshots,
  reclaim versions older than the oldest active snapshot, and preserve
  tombstones until they are safe to remove.
- [ ] **Lifecycle cleanup** — release transaction snapshots on commit,
  rollback, client disconnect, and server errors.
- [ ] **Tests** — add coverage for repeatable reads, read-your-writes,
  invisible uncommitted writes, rollback, restart recovery, conflicts,
  concurrent clients, and snapshot-safe compaction.
- [ ] **Documentation** — document transaction isolation, conflict behavior,
  WAL recovery, and compaction guarantees in `README.md` and
  `SQL_COMMANDS.md`.
