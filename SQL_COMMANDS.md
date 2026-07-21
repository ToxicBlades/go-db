# SQL Commands

This document describes the SQL supported by `go-db`. The SQL implementation is
intentionally small and is handled by `sql/sql.go`.

## Supported commands

### Explicit transactions

`BEGIN` starts a transaction that remains active across requests. Changes are
staged privately and made durable by `COMMIT`; `ROLLBACK` discards the staged
writes:

```sql
BEGIN;
INSERT INTO users (id, name, active) VALUES (4, 'Drew', true);
ROLLBACK;
```

Only one explicit transaction may be active per client connection. `COMMIT`
and `ROLLBACK` outside a transaction return an error.

Transactions use snapshot reads and read their own writes. Requests from
different clients can interleave. A commit that writes a key changed after
`BEGIN` fails with a transaction conflict; the transaction remains open so it
can be rolled back. Disconnecting a client rolls back its open transaction.

### `EXPLAIN`

Shows the executor's query plan, including for simple queries that use a
sequential scan:

```sql
EXPLAIN SELECT * FROM users WHERE active = true;
```

`EXPLAIN` supports `SELECT`, `INSERT`, `UPDATE`, and `DELETE` and does not
execute the wrapped statement.

Use `EXPLAIN TABLE` to inspect a table's columns and constraints:

```sql
EXPLAIN TABLE users;
```

### `SHOW TABLES`

Lists all tables registered with the database.

```sql
SHOW TABLES;
```

The result contains one column, `table_name`.

### `LIST TABLES`

An alternative spelling of `SHOW TABLES`.

```sql
LIST TABLES;
```

### `SELECT`

Reads rows from a table.

```sql
SELECT column1, column2 FROM table_name;
```

Use `*` to select every column:

```sql
SELECT * FROM users;
```

#### Filtering with `WHERE`

Conditions support `=`, `!=`, `<`, `>`, `<=`, and `>=`, combined with `AND`,
`OR`, and parentheses. `AND` binds more tightly than `OR`:

Equality filters on non-key columns use automatically maintained secondary
indexes. Other predicates continue to use a sequential scan.

```sql
SELECT name, active
FROM users
WHERE active = true AND (id >= 1 OR name != 'Bob');
```

The comparison is performed against the displayed value of the stored value.

#### Ordering and pagination

`ORDER BY` sorts by a selected column (ascending by default; use `DESC` for
descending order). `LIMIT` restricts the result count and `OFFSET` skips rows:

```sql
SELECT id, name FROM users ORDER BY id DESC LIMIT 10 OFFSET 5;
```

#### Aggregates and grouping

`COUNT`, `SUM`, `AVG`, `MIN`, and `MAX` are supported. Aggregate expressions
are returned using their expression as the result column name:

```sql
SELECT active, COUNT(*), AVG(id)
FROM users
GROUP BY active
ORDER BY active;
```

`SUM` and `AVG` require numeric columns. `COUNT(*)` counts rows; other
aggregates ignore NULL values. A naive nested-loop inner join is supported:

```sql
SELECT users.name, orders.total FROM users JOIN orders
ON users.id = orders.user_id;
```

`INNER JOIN` is accepted too. Multiple semicolon-separated statements in one
request execute as a transaction; row changes are rolled back if a statement
fails.

### `INSERT`

Adds one row to a table. Column names and values must be supplied explicitly.

```sql
INSERT INTO users (id, name, active)
VALUES (1, 'Ada', true);
```

The first column in a table schema is its key column. Every `INSERT` must
include that column. An existing key is handled according to the table/store
implementation and may replace the existing row.

The number of columns must match the number of values:

```sql
INSERT INTO users (id, name, active)
VALUES (2, 'Bob', false);
```

### `CREATE TABLE`

Creates a table with typed columns. Supported types are `INT`, `FLOAT` (also
`REAL`/`DOUBLE`), `STRING` (or `TEXT`), `BOOL` (or `BOOLEAN`), `BYTES` (or
`BLOB`), and `TIMESTAMP`. Timestamp values use RFC3339 strings such as
`'2026-07-21T12:00:00Z'`. Columns may
be followed by `NOT NULL`, `UNIQUE`, or `REFERENCES table(column)`. `NULL` is
accepted as a value unless `NOT NULL` is declared; NULL values do not count as
duplicates for `UNIQUE`.

If the statement does not declare an `id` column, an `id INT` column is added
automatically as the first column. When inserting into such a table, omit the
`id` column to have the executor assign the next integer ID.

```sql
CREATE TABLE users (id INT, name STRING, active BOOL);

CREATE TABLE orders (id INT, user_id INT NOT NULL REFERENCES users(id),
                     total FLOAT, external_id STRING UNIQUE);
```

### `ALTER TABLE`

The supported schema changes are adding, dropping, and renaming columns:

```sql
ALTER TABLE users ADD COLUMN email STRING;
ALTER TABLE users DROP COLUMN email;
ALTER TABLE users RENAME COLUMN name TO display_name;
```

New columns receive their type's zero value in existing rows.

### `DROP TABLE`

Removes a table from the executor:

```sql
DROP TABLE users;
```

### `UPDATE`

Updates one or more columns. An optional `WHERE` condition limits the
rows affected. Without `WHERE`, every row is updated.

```sql
UPDATE users SET active = true WHERE id = 1;
UPDATE users SET name = 'Ada', active = true;
```

### `DELETE`

Deletes rows using an optional condition. Without `WHERE`, all rows
are deleted.

```sql
DELETE FROM users WHERE id = 1;
DELETE FROM users;
```

## Values

The parser accepts these literal types:

| Type | Examples |
| --- | --- |
| Integer | `0`, `1`, `-1` is not supported by the lexer |
| String | `'Ada'`, `"Ada"` |
| Boolean | `true`, `false` |

Strings may contain escaped characters using a backslash. Identifiers may
contain letters, digits, and underscores.

## Statement rules

- Keywords and table/column names are matched case-insensitively where the SQL
  executor performs name lookup.
- A trailing semicolon is optional for a single statement.
- A request may contain multiple semicolon-separated statements. Such batches
  are atomic; row changes are rolled back if a statement fails. `BEGIN`,
  `COMMIT`, and `ROLLBACK` can also be used across separate requests.
- `SELECT` requires `FROM` and `INSERT` requires `INTO`, a column list, and
  `VALUES`.
- Selecting an unknown column or table returns an error.

## Tables

Tables may be created with `CREATE TABLE`, or created by the Go application
when the server starts. Table names, schemas, constraints, and backing paths
are stored in a catalog sidecar next to the database and are reopened when the
server restarts. In the default setup, the available table is `users` with the
schema `users(id INT, name STRING, active BOOL)`.

Backups include the database, WAL, index, and catalog sidecars. The source
database must be closed before running `go-db backup`; backup and restore
validate the on-disk files before replacing the destination.

## Running SQL

### Prepared statements

Go callers can parse a parameterized statement once and execute it repeatedly.
Use `?` placeholders; arguments are bound as values and are never interpreted
as SQL text:

```go
stmt, err := executor.Prepare("SELECT name FROM users WHERE id = ?")
result, err := stmt.Execute(42)
```

Prepared statements are available through the Go API. The number of arguments
must match the number of placeholders.

Start the server and load the optional seed file:

```bash
go run ./cmd/go-db server --db db/go-db.db --addr :5433 --seed seed.sql
```

Connect with the interactive SQL client:

```bash
go run ./cmd/go-db sql --addr :5433
```

When the server is started with `--user` and `--password`, pass the same
options to the client. The client authenticates before sending SQL; failed
authentication closes the connection.

The server also accepts newline-delimited plain SQL or JSON requests such as:

```json
{"query":"SELECT * FROM users WHERE active = true;"}
```

Type `exit` or `quit` in the interactive client to disconnect.

## Complete example

```sql
SHOW TABLES;

INSERT INTO users (id, name, active)
VALUES (3, 'Casey', true);

SELECT * FROM users;

SELECT name FROM users WHERE id = 3;

UPDATE users SET active = false WHERE id = 3;

DELETE FROM users WHERE id = 3;
```
