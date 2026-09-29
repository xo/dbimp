# D53. The Go types of SurrealDB

Status: Amended by D70 and D113.

Ken accepted this on 2026-09-27. It decides item 4 of step 9 for SurrealDB. D70 gives a `RecordID` a
text form. In CBOR, the driver decodes each value by its tag:

- An integer is an `int64`, a float is a `float64`, and a decimal (tag 10) is
  an `*apd.Decimal` (D33).
- A string is a `string`, a boolean is a `bool`, and bytes are `[]byte`.
- A datetime (tags 0 and 12) is a `time.Time` in UTC, with its nanoseconds.
- A duration (tags 13 and 14) is a `time.Duration`. A duration longer than
  a `time.Duration` holds, about 292 years, is an error.
- A UUID (tags 9 and 37) is a `uuid.UUID` (D25).
- A record id (tag 8) is a `surrealdb.RecordID`, with the table and the key.
  Its `String` method writes it as SurrealQL does, such as `person:tobie`.
  Ken decided this on 2026-09-27.
- An array and a set (tag 56) are `[]any`, and an object is
  `map[string]any`.
- A geometry (tags 88 to 94) is the `map[string]any` of its GeoJSON, as the
  server writes it in JSON.
- A table (tag 7), a range (tags 49 to 51), a file (tag 55) and a future (tag
  15) are strings, in the form that SurrealQL writes them.
- NONE (tag 6) and NULL are nil (D52).

An argument is encoded the other way, so a `time.Time`, a `uuid.UUID`, a
`RecordID` and an `*apd.Decimal` keep their types on the server.
