# D206. The types of the Exasol driver

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 4. The type table of [EXASOL.md](../EXASOL.md) is the mapping, and the code makes the same table (step 10 of DRIVER.md).

- A DECIMAL with scale 0 and a precision of 18 or less is an `int64`. Every other DECIMAL is an `*apd.Decimal` (D33). The Go type depends on the column and not on the value (D183).
- A DOUBLE is a `float64`. A CHAR and a VARCHAR are a `string`, and a CHAR keeps the padding that the server sends.
- A DATE is a `dbimp.Date`. A TIMESTAMP is a `dbimp.LocalDateTime`. A TIMESTAMP WITH LOCAL TIME ZONE is a `time.Time` in the time zone of the session (D138).
- Both intervals are a `dbimp.Interval`. A value of INTERVAL YEAR TO MONTH that does not fit an `int32` of months is an error, and the rows before it are read.
- A GEOMETRY is its text in WKT, as a `string`. A HASHTYPE is a `[]byte` for every size, and it is no `uuid.UUID`. A BOOLEAN is a `bool`.
