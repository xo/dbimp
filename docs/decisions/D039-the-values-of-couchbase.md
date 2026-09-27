# D39. The values of Couchbase

Status: Amended by D44.

Ken accepted this on 2026-09-27. D44 amends how a string scans into a byte
slice.

The signature names the kind of each column, and the driver decodes each
value by what it holds, with `ColumnTypeDatabaseTypeName` the kind in upper
case: `NUMBER`, `STRING`, `BOOLEAN`, `NULL`, `MISSING`, `ARRAY`, `OBJECT` or
`JSON`.

- A number is an `int64` when it is an integer that fits, and a `float64`
  otherwise, as `dbimp.Number` decodes it. The server sends a larger integer
  as a float64 itself, so no `*apd.Decimal` arrives from Couchbase.
  Correction of 2026-09-27: the server rounds a larger integer to a float64,
  and sends the digits of the rounded value, such as
  `123456789012345680000000000000`, with no point and no exponent. Those
  digits are too large for an `int64`, so `dbimp.Number` returns them as an
  `*apd.Decimal`, which holds exactly what the server sent. A test of the
  driver found this.
- A string is a `string`, a boolean is a `bool`, and a time, a binary value
  and a UUID are the strings that the server sends, because SQL++ has no
  type for them. A caller scans a time into a `time.Time` through
  `database/sql`.
- An array is a `[]any` and an object is a `map[string]any`, as `dbimp.Any`
  decodes them. A scan into a `*[]byte` or a `*jsontext.Value` gets the JSON
  text of the value instead, so a caller that wants the text keeps it.
- NULL and MISSING are both nil, as n1ql D24 decided, because
  `database/sql` has one NULL. A caller that must tell them apart reads the
  kind of the column.
- On 7.2.9 the columns arrive in the order of their names, and the driver
  returns that order, as n1ql D33 decided. It does not send a second request
  to learn the order of the projection.
