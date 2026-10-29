# D223. The Tablestore driver types a column from the FlatBuffers answer

Status: Decided. Ken reviewed the table on 2026-10-29, except the row of `TIMESTAMP`.

The FlatBuffers rows of an answer carry the name and the type of each column before the first
row. Decided:

1. The driver asks for the payload version 2 (FlatBuffers) and never for the plain buffer.
2. The Go type of each column is the one of the type table of `docs/TABLESTORE.md` (D135 and
   D137). `DATETIME` is a `dbimp.LocalDateTime`, which Ken decided on 2026-10-29. The Go type of
   `TIMESTAMP` is `time.Time` until step 14a shows the wire form.
3. A NULL is a flag of the value in the FlatBuffers row. It reaches `database/sql` as nil, and
   never as a zero value (D18).
4. The mapping accepts the column types `BIGINT`, `DOUBLE`, `BOOL`, `MEDIUMTEXT`, `MEDIUMBLOB`,
   `DATETIME`, `DATE` and `TIMESTAMP`. A table that stores a NaN or an Infinity cannot exist.

See `docs/TABLESTORE.md`, section "Types".
