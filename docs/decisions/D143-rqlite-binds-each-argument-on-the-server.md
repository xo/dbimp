# D143. rqlite binds each argument on the server

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the rqlite driver. This is item
6 of step 9 of [DRIVER.md](../DRIVER.md) for rqlite. The server binds `?`,
`?NNN`, `:name`, `$name` and `@name` (RQLITE.md, Parameters), so the driver
sends each argument as a JSON value after the statement, and a named
argument in an object.

| Go value | JSON value | Binds as |
| --- | --- | --- |
| nil | `null` | NULL |
| `int64`, and a `uint64` up to 9223372036854775807 | a number | INTEGER |
| `float64` | a number with a fraction or an exponent, so 1 is `1.0` | REAL |
| `bool` | `true` or `false` | INTEGER 1 or 0 |
| `string` | a string | TEXT |
| `[]byte` | an array of numbers | BLOB |
| `time.Time` | RFC 3339 text, to the nanosecond | TEXT |
| `dbimp.Date`, `dbimp.LocalTime`, `dbimp.LocalDateTime` | their ISO 8601 text | TEXT |

- A `uint64` above 9223372036854775807, a NaN and an infinity are errors,
  because the server would bind a REAL, or JSON has no form for them.
- The server binds a string of the form `x'...'` as a BLOB (recorded). The
  driver writes such a string into the text of the statement as a literal
  of SQL, with each quote doubled, through the parser of placeholders of
  the root package (D34), so that it stays text.
