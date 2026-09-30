# D135. A value has the Go type that fits the type that its database names

Status: Amended by D138.

Ken decided this on 2026-09-30, for every driver, when he kept D130 for the
`JSON` column that Pinot names `STRING`. Gemini and DeepSeek reviewed the
wording the same day. Both asked for a fixed table in place of "best
fits", and for a rule for a type that has no Go type.

A value that a caller scans into `*any`, and a value that `Rows.Next`
returns, has the Go type that fits the type that the database names for
its column:

| The type that the database names | The Go type |
| --- | --- |
| an integer | `int64`, or `uint64` for an unsigned type above the range of `int64` |
| a float | `float64` |
| a decimal | `*apd.Decimal` (D33) |
| a boolean | `bool` |
| a string | `string` |
| a binary value | `[]byte` |
| a date, a time or a timestamp | `time.Time` |
| a duration | `time.Duration` |
| a UUID | `uuid.UUID` (D25) |
| an array or a list | `[]any` of the Go types of its elements |
| a map, an object, a document or JSON | the decoded value: nil, `bool`, `string`, a number, `[]any` or `map[string]any` |
| NULL | nil, for every type |

- A column that the database names as a string gives a `string`, even when
  its text looks like a number, a time or JSON. So the `JSON` column that
  the multi-stage engine of Pinot names `STRING` gives its text (D130).
- When the database names no type, the driver takes the type from the
  encoding of the value, such as the kind of a JSON token or the tag of a
  CBOR item. A JSON number with no type decodes as `dbimp.Number` does
  (D19).
- A driver never takes a type from the text of a string, and never returns
  the text of JSON, or of another encoding, in place of the value that it
  holds.
- A type that has no Go type, such as a geometry, gives the form that the
  decision of its driver names, such as WKT for Databend (D119).
- A value that the driver cannot decode is an error, and never its raw
  text.
- `ColumnTypeScanType` names the same Go type.

This makes hard rule 3 of AGENTS.md precise, and the type table of each
product document (step 10 of DRIVER.md) holds it for each type.
