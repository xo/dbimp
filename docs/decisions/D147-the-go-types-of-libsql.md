# D147. The Go types of libSQL

Status: Decided.

Ken decided this on 2026-10-01, at step 8a of the libSQL driver. This is
item 4 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL.

- D140 applies. The Go type of a column follows the rules of affinity of
  SQLite for its `decltype`, and a value whose form cannot have that Go type
  keeps the Go type of its storage class. Hrana names the storage class of
  each value (LIBSQL.md, Types), so the fallback is exact.
- The server converts nothing, so the driver reads four declared types
  itself:
  - `BOOLEAN` is a `bool` from an integer: 0 is false, and any other is
    true.
  - `DATE` is a `dbimp.Date` from text of the form `YYYY-MM-DD`.
  - `DATETIME` is a `dbimp.LocalDateTime` from text with no zone.
  - `TIMESTAMP` is a `time.Time` from RFC 3339 text, and from text with no
    zone in UTC, as `mattn/go-sqlite3` reads it.
  Text that does not parse, and a value of another storage class, keep
  their own Go type.
- `F32_BLOB` is a `dbimp.Vector[float32]` from the little-endian bytes of the
  BLOB. A BLOB whose length is not a multiple of 4 keeps `[]byte`.
- An infinity arrives as a float with a value of null, so the driver gives
  `+Inf`, whose sign may be wrong. `CAST(x AS TEXT)` in the statement gives
  `Inf` or `-Inf`. DeepSeek proposed an error, and Gemini `+Inf`.
- `ANY`, and a column with no declared type, read by the storage class of
  each value (D140).
