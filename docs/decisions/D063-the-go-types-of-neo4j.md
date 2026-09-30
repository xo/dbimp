# D63. The Go types of Neo4j

Status: Amended by D138 and D139.

Ken chose the temporal types and accepted this on 2026-09-27. It decides items
4 and 5 of step 9 for Neo4j. The driver decodes each value by its `$type`:

- `Integer` is an `int64`, read from its string, and `Float` is a
  `float64`, with `NaN`, `Infinity` and `-Infinity` (D19).
- `String` is a `string`, `Boolean` is a `bool`, and `Base64` is a
  `[]byte`.
- `List` is a `[]any`, and `Map` is a `map[string]any`.
- `OffsetDateTime` and `ZonedDateTime` are a `time.Time`. A zone name, such
  as `Europe/Oslo`, becomes the `Location` of that name. If the system has
  no such zone, the `Location` is a fixed zone with the name and the offset
  that the server sent.
- `Date`, `LocalTime`, `Time` and `LocalDateTime` are `neo4j.Date`,
  `neo4j.LocalTime`, `neo4j.Time` and `neo4j.LocalDateTime`. Each is a
  defined type of `time.Time`, as in the Go driver of Neo4j. Each scans into
  a `*time.Time` too. The driver parses them itself, because `time.Parse`
  takes a year of four digits only, and a date of Neo4j runs from the year
  -999999999 to 999999999 (measured).
- `Duration` is a `neo4j.Duration`, with `Months`, `Days`, `Seconds` and
  `Nanos`, because a `time.Duration` holds no months. Its `String` method
  writes the form of the server, such as `P1Y2M3DT4H5M6.007S`.
- `Point` is a `neo4j.Point`, with `SRID`, `X`, `Y` and `Z`, and `Dims`,
  which is 2 or 3.
- `Node`, `Relationship` and `Path` are `neo4j.Node`, `neo4j.Relationship`
  and `neo4j.Path`, with the fields of typed JSON: the element ids, the
  labels or the type, and the properties as a `map[string]any`.
- `Vector` is a `neo4j.Vector`, which holds a `[]float32`, a `[]float64`, an
  `[]int8`, an `[]int16`, an `[]int32` or an `[]int64` by its
  `coordinatesType`.
- `UUID` is a `uuid.UUID` (D25).
- `Null` is nil. A property that a node does not have reads as NULL, so NULL
  and a missing value are one value (measured).
- `Unsupported`, and a `$type` that the driver does not know, is an error.

Neo4j has no decimal type (measured). An `*apd.Decimal` argument is refused,
and a caller converts it to a string or a float.

An argument is encoded the other way, so each type above keeps its type on
the server. A `time.Time` is a `ZonedDateTime` when its `Location` has a
name other than `UTC` and `Local`, and an `OffsetDateTime` when it has not.
An `int` and each other integer type is an `Integer`, and an integer that
does not fit in an `int64` is refused. A `[]float32` or any other slice is a
`List`, and only a `neo4j.Vector` is a `Vector`.
