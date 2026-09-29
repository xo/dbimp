# D132. Pinot arguments are written into the text

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the Apache Pinot driver.
This is item 6 of step 9 of [DRIVER.md](../DRIVER.md) for Apache Pinot. The
Broker binds no parameters: its body has no field for them, and the Java
and the Go clients write each value into the text ([PINOT.md](../PINOT.md)).
So the driver finds each `?` with the parser for placeholders of the root
package (D34), which skips a `?` inside a literal or a comment, and writes
each argument as a literal of Pinot:

- `nil` is `NULL`, a `bool` is `true` or `false`, an `int64` and a `uint64`
  are their digits, and a `float64` is its shortest text. A `NaN` or an
  infinity is refused, because Pinot has no literal for it.
- A `string` is in single quotes, with each `'` doubled.
- A `*apd.Decimal` is `CAST('<digits>' AS BIG_DECIMAL)`, which keeps every
  digit.
- A `time.Time` is its milliseconds in UTC, as a `LONG`, which Pinot
  compares with a `TIMESTAMP`.
- A `[]byte` is `hexToBytes('<hex>')`.

A named argument is refused, because Pinot has no named placeholder.
