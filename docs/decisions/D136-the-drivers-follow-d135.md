# D136. The drivers follow D135

Status: Amends D39 and D119, and amended by D138.

Ken decided this on 2026-09-30, when he accepted the five changes that an
audit of the seven drivers against D135 found. Each change holds in its
driver, and a test holds each one.

- A type that has no Go type keeps the text that its server writes, as the
  decision of its driver named it: the table, the range, the file and the
  future of SurrealDB (D53), the interval of Databend (D118), and the time
  of day and the interval of InfluxDB. Go has no type for a range, a time
  of day, or an interval of months.
- A Databend geometry and geography follow `geometry_output_format`, at the
  top of a value and inside an Array, a Map or a Tuple alike: WKT and EWKT
  as their text (D119), WKB and EWKB as the `[]byte` of their hex, and
  GeoJSON as the decoded JSON value. The driver sends WKT, so a caller gets
  WKT unless it changes the setting (measured on 1.2.948).
- An InfluxDB `Duration` is a `time.Duration`. The server writes it in ISO
  8601 as seconds, such as `PT1.5S`, and zero as `P0D` (measured on
  3.11.5).
- A Couchbase value whose JSON kind is not the kind that its signature
  names is an error, so that no value has another Go type than
  `ColumnTypeScanType`. None of the 1,092 values of the recordings is one.
  This amends D39, which decides each value by its own kind.
- `ColumnTypeScanType` names the Go type of the value, `T`, for a column
  that can hold a NULL too, and `ColumnTypeNullable` says that it can. A
  caller can still scan into `sql.Null[T]` (D25). InfluxDB named
  `sql.Null[T]`, and now names `T`, as every other driver does.
