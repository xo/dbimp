# D130. The Go types of Pinot, and NULL

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the Apache Pinot driver. This
is items 4, 5 and 12 of step 9 of [DRIVER.md](../DRIVER.md), and step 10
writes the type table from it. `dataSchema` names the type of each column
([PINOT.md](../PINOT.md)).

- Each query sends `enableNullHandling=true`, so a NULL that the table
  stores arrives as JSON `null`, and reaches `database/sql` as nil (hard rule
  3). Without it, a NULL arrives as the default value of its type, such as
  `-2147483648` or `"null"` (measured). A table whose schema stores no NULL
  still gives its default values, which the server writes, and the driver
  cannot tell them from a value.

| Type of the server | Go type |
| --- | --- |
| `INT`, `LONG` | `int64` |
| `FLOAT`, `DOUBLE` | `float64`, with `"NaN"`, `"Infinity"` and `"-Infinity"` |
| `BIG_DECIMAL` | `*apd.Decimal`, with every digit (D33) |
| `BOOLEAN` | `bool` |
| `STRING` | `string` |
| `JSON` | the decoded JSON value: nil, bool, string, a number as `dbimp.Number` gives it, `[]any` or `map[string]any` |
| `BYTES` | `[]byte`, from hex |
| `TIMESTAMP` | `time.Time`, in UTC, the zone of the server in the image |
| `MAP` | `map[string]any` |
| an array, such as `INT_ARRAY` | `[]any` of the Go type of its element |
| `UNKNOWN` | nil, the type of a `NULL` literal |

The driver reads JSON, and no binary encoding (item 12, D13).
