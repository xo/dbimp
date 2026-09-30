# D152. libSQL binds typed arguments on the server

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the libSQL driver. This is items
5, 6, 9 and 12 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL.

- NULL and a value that is missing are one value, because SQLite has no
  value that is missing (D18).
- The server binds each argument, as a typed value of Hrana: an `int64`, and
  a `uint64` up to 9223372036854775807, as an integer in a string, a
  `float64` as a JSON number, a `string` as text, a `[]byte` as a BLOB in
  base64, a `bool` as the integer 1 or 0, and a `time.Time` or a civil type
  of the root package as its text. A `uint64` above that range, a NaN and an
  infinity are errors, because the server refuses them (recorded).
- A named argument goes in `named_args`. A statement with positional and
  named arguments together is an error before anything is sent, because
  the server refuses it with `ARGS_BOTH_POSITIONAL_AND_NAMED` (recorded).
- A cancelled context stops the statement, because the server stops it when
  the client leaves (measured). Hrana has no request that cancels one.
- The driver speaks JSON, and no protobuf (D13).
- `RowsAffected` is `affected_row_count`. `LastInsertId` is
  `last_insert_rowid`, and 0 when the server sends null.
