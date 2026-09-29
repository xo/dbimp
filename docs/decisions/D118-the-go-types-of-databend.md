# D118. The Go types of Databend

Status: Decided.

Ken decided this on 2026-09-29, at step 9 of the Databend driver.
This is items 4 and 5 of step 9 of [DRIVER.md](../DRIVER.md) for Databend,
and step 10 writes the type table from it. Every value arrives as a JSON
string or `null`, and `schema` names the type of each column
([DATABEND.md](../DATABEND.md)). The driver decodes each value by that type:

| Type of the server | Go type |
| --- | --- |
| `Int8` to `Int64`, and `UInt8` to `UInt32` | `int64` |
| `UInt64` | `int64`, or `*apd.Decimal` above the range of `int64`, as Couchbase does |
| `Float32`, `Float64` | `float64`, with `Infinity`, `-Infinity` and `NaN` |
| `Decimal(p, s)` | `*apd.Decimal`, with every digit (D33) |
| `Boolean` | `bool` |
| `String` | `string` |
| `Binary` | `[]byte`, from hex |
| `Date` | `time.Time` at midnight in UTC |
| `Timestamp` | `time.Time`, in the timezone of `settings.timezone` of the response |
| `Timestamp_Tz` | `time.Time`, with its offset |
| `Interval` | `string`, as the server writes it, because a month has no fixed length |
| `Variant` | the decoded JSON value, by the rules of D18 |
| `Vector(n)` | `[]float32` |
| `Geometry`, `Geography` | `string`, in WKT, which the driver asks for with `geometry_output_format` |
| `Array`, `Map`, `Tuple`, `Bitmap` | D119 |

A NULL is JSON `null`, and reaches `database/sql` as nil (item 5, hard rule
3). A missing value does not occur, because each row is an array with a
value for each column. The driver sends `format_null_as_str=0` with each
statement, because with 1 a NULL is the string `NULL`, which the text
`NULL` shares. A JSON null inside a `Variant` is a value, and not a NULL of
the column.
