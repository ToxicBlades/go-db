## Transactions / concurrency

- **MVCC** — more ambitious, but a great learning exercise given you already have append-only records with a "latest live value" model.

## Storage engine
- **Secondary indexes** — right now it looks like the key column is the only indexed path; a secondary index structure would let `WHERE` on non-key columns be fast instead of a full scan.

## Server / client
- **Authentication** on the TCP server.
