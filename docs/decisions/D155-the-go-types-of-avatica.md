# D155. The Go types of Avatica

Status: Decided.

Ken decided this on 2026-10-01, at step 8a of the Avatica driver. This is
item 4 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.

- The driver reads each value by the name of its type of JDBC, and not by
  its `rep`, because the two disagree: a `UUID` has the `rep` `BYTE_STRING`
  and arrives as its text, and a `DECIMAL` of Phoenix has the `rep` `OBJECT`
  (AVATICA.md, Types).
- The integers, and `UNSIGNED_INT` and `UNSIGNED_LONG` of Phoenix, which
  holds 0 to 9223372036854775807, are `int64`. `DECIMAL` is an
  `*apd.Decimal` from the digits of the JSON number. `DOUBLE` and `FLOAT`
  are `float64`, with `"Infinity"`, `"-Infinity"` and `"NaN"`. `BOOLEAN` is
  a `bool`. `CHAR` and `VARCHAR` are a `string`, as the server sends it.
  `BINARY` and `VARBINARY` are a `[]byte` from base64.
- `DATE` is a `dbimp.Date` from its days since 1970-01-01, `TIME` a
  `dbimp.LocalTime` from its milliseconds since midnight, and `TIMESTAMP` a
  `dbimp.LocalDateTime` from its milliseconds since 1970-01-01 read in UTC,
  because the server sends a time with no zone as if it were in UTC.
- An `INTERVAL` of HSQLDB is a `dbimp.Interval` from its text. `ARRAY` is a
  `[]any` of the Go types of its elements. `UUID` is a `uuid.UUID` (D25).
- `TIMESTAMP WITH TIME ZONE`, `TIME WITH TIME ZONE`, `CLOB` and `BLOB` have
  no Go type, because the server fails the whole answer for them. Its error
  reaches the caller.
