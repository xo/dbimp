# D138. The root package defines the types that Go lacks

Status: Amends D63, D118, D135 and D136, and amended by D139.

Ken decided this on 2026-09-30, from the table of [TYPES.md](../TYPES.md)
and the reviews of Gemini and DeepSeek. W21 carries it out.

Four kinds appear in at least two drivers, and Go has no type for any of
them. A `time.Time` gives a date, a time of day and a timestamp with no zone
a zone that they do not have, and a `time.Duration` cannot hold a month. So
the package `dbimp` defines each one, with the standard library only:

| Kind | Type |
| --- | --- |
| date | `type Date struct{ Year int; Month time.Month; Day int }` |
| time of day | `type LocalTime struct{ Hour, Minute, Second, Nanosecond int }` |
| local timestamp | `type LocalDateTime struct{ Date Date; Time LocalTime }` |
| interval | `type Interval struct{ Months, Days int32; Nanoseconds int64 }` |

- Ken asked both models for a better name than `TimeOfDay`, and both chose
  `LocalTime`. It pairs with `LocalDateTime`, it is the name of java.time and
  of Neo4j, and `dbimp.Time` would read like `time.Time`.
- Ken asked whether `LocalTimestamp` names the third type better. Both
  models kept `LocalDateTime`: a Go reader takes a timestamp for an instant,
  and the type is a `Date` and a `LocalTime`, as in java.time, JDBC 4.2 and
  Neo4j.
- Ken asked whether to import `cloud.google.com/go/civil`, and to name its
  types with aliases. He chose to write the types here: `civil` has no
  interval, which dbimp writes anyway, its types are not an exact match,
  and it is a package of the root module `cloud.google.com/go`, whose graph
  of about 50 modules would reach every consumer (measured on 2026-09-30).
- The four types are the shapes of the civil types of Go libraries and of
  java.time. `Interval` is the shape of `pgtype.Interval` of pgx and of the
  Arrow type `Interval(MonthDayNano)`, with nanoseconds where PostgreSQL
  keeps microseconds.
- Each type has a method `String` that writes ISO 8601, such as
  `2026-09-30`, `12:30:00.5`, `2026-09-30T12:30:00.5` and `P1M2DT3.5S`, and
  a function that reads that text back. `Date`, `LocalTime` and
  `LocalDateTime` each have a method that gives the `time.Time` of the value
  in a location that the caller names.
- `dbimp.Assign` gives a caller that scans into a `*string` the text of
  `String`. A caller that scans a `Date`, a `LocalTime` or a
  `LocalDateTime` into a `*time.Time`, or an `sql.Null[time.Time]`, gets the
  value in UTC, on 0000-01-01 for a `LocalTime`, as lib/pq does for a time
  of day.
- A `LocalTime` has an hour from 0 to 23. A server that writes `24:00:00`,
  as PostgreSQL can, needs a rule of its own driver.
- A Neo4j duration whose seconds pass the range of an `int64` of
  nanoseconds, about 292 years, is an error, and never a value cut short.

Two rules go with the types:

- An unsigned integer of 64 bits is always a `uint64`, whatever its value,
  because D135 follows the type and not the value. The Databend `UInt64`
  changes from an `int64`, or an `*apd.Decimal` above the range of `int64`,
  and D118 is amended.
- A vector is a slice of the Go type of its elements, such as `[]float32`.
  The Neo4j vector changes from `neo4j.Vector` to that slice. As an
  argument, a slice stays a list, as D63 says, so a caller still sends a
  vector as a `neo4j.Vector`.

The drivers change so:

| Driver | Before | After |
| --- | --- | --- |
| Neo4j | `neo4j.Date`, `neo4j.LocalTime`, `neo4j.LocalDateTime`, `neo4j.Duration`, `neo4j.Vector` | `dbimp.Date`, `dbimp.LocalTime`, `dbimp.LocalDateTime`, `dbimp.Interval`, and a slice for a vector. D63 is amended |
| Databend | a `Date` as a `time.Time` at midnight in UTC, an `Interval` as text, a `UInt64` as an `int64` or an `*apd.Decimal` | `dbimp.Date`, `dbimp.Interval`, and a `uint64`. D118 is amended |
| InfluxDB | a `Date32` as a `time.Time` at midnight in UTC, a `Time64` and an `Interval(MonthDayNano)` as text | `dbimp.Date`, `dbimp.LocalTime` and `dbimp.Interval` |

D135 is amended, because its table named a `time.Time` for a date, a time
and a timestamp with no zone. D136 is amended, because it kept a time of day
and an interval as text. Each driver accepts the four types as arguments
too. The change needs a release, because a caller of each of the three
drivers gets a new Go type.

These stay as they are, because only one driver has each one, or because
the databases differ in what the type holds:

- A time of day with an offset stays `neo4j.Time`.
- A geometry keeps the form that the decision of its driver names.
- A range stays the text of the server, and a record id stays the type of
  its driver.
- A node, a relationship and a path stay the types of the Neo4j driver.
