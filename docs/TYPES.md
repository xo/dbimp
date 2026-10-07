# Types

This file maps every type of every driver onto one list of kinds, and each
kind onto its Go type. A kind is a family of types that means the same thing
in any database, such as an integer or a date. The rule is D135: a value has
the Go type that fits the type that its database names. D136 holds the
choices of each driver under that rule. D137 makes this file, and step 8a of
[DRIVER.md](DRIVER.md) maps the types of a new driver here before its code
is written. D138 and D139 define the types that the root package holds for
every driver, where Go has none.

## The kinds

The first column is the name that the type table of each product document
uses in its column Kind. `TestEveryTypeHasAKind` fails for a kind that this
list does not name. A new kind needs a decision.

<!-- dbimp:kinds -->
| Kind | What it is | Go type (D135, D136 and D138) |
| --- | --- | --- |
| null | A NULL, and a value that is missing, such as `MISSING` or `NONE` | nil |
| boolean | True or false | `bool` |
| integer | A signed integer, or an unsigned integer whose type fits in `int64` | `int64` |
| unsigned integer | An unsigned integer of 64 bits | `uint64`, whatever its value (D138) |
| number | A number whose type the database does not name, such as a JSON number | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`, as `dbimp.Number` decodes it (D19) |
| float | A floating point number | `float64` |
| decimal | A number with a fixed or an exact precision | `*apd.Decimal` (D33) |
| big integer | An integer of 128 or 256 bits, which does not fit in `int64` or `uint64` | `*big.Int` (D177) |
| string | Text | `string` |
| binary | Bytes | `[]byte` |
| date | A day of the calendar, with no time and no zone | `dbimp.Date` (D138) |
| time of day | A time of day, with no date and no zone | `dbimp.LocalTime` (D138) |
| time of day with offset | A time of day, with an offset from UTC | `dbimp.OffsetTime` (D139) |
| local timestamp | A date and a time, with no zone | `dbimp.LocalDateTime` (D138) |
| timestamp | An instant, with or without a zone that the server names | `time.Time` |
| duration | An exact length of time | `time.Duration` |
| interval | A length of the calendar, with months and days | `dbimp.Interval` (D138) |
| uuid | A UUID | `uuid.UUID` (D25) |
| ip address | An IPv4 or an IPv6 address | `netip.Addr` (D177) |
| json | A value of any JSON shape, such as a variant or a JSON column | the decoded value: nil, `bool`, `string`, a number, `[]any` or `map[string]any` |
| array | A list of values | `[]any`, of the Go types of its elements |
| set | A list of values with no order | `[]any` |
| map | Keys and values, such as a map, an object or a document | `map[string]any` |
| tuple | Values in the order of their fields | `[]any` |
| vector | A list of numbers of one type, for a search by similarity | `dbimp.Vector[T]`, a slice of the Go type of its elements, such as `dbimp.Vector[float32]` (D139) |
| geometry | A shape or a point | the form that the decision of the driver names |
| range | Two bounds | the text of the server |
| record id | A reference to a record, with its table and its key | the type of the driver |
| node | A node of a graph | the type of the driver |
| relationship | An edge of a graph | the type of the driver |
| path | A path through a graph | the type of the driver |
| bitmap | A set of integers in a compressed form | an error, where JSON carries no bytes of it |
| other | A type that has no Go type and no kind above | the text of the server |
<!-- /dbimp:kinds -->

## The drivers

`TestTheTypeMatrixIsCurrent` writes this table from the type table of each
product document, and fails when it is stale. Run it with `DBIMP_UPDATE=1`
to write it. Each cell holds the wire types of the driver for the kind, each
with the start of its Go type. The product document holds the whole Go type,
the scan type and the database type.

<!-- dbimp:matrix -->
| Kind | arangodb | avatica | couchbase | databend | druid | influxdb | libsql | neo4j | pinot | rqlite | surrealdb | trino |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| null | null: `nil` |  | missing: `nil`<br>null: `nil` |  | NULL: `nil` | null: `nil` |  | null: `nil` |  |  | none: `nil`<br>null: `nil` | UNKNOWN: `nil` |
| boolean | boolean: `bool` | BOOLEAN: `bool` | boolean: `bool` | boolean: `bool` | BOOLEAN: `bool` | boolean: `bool` | BOOLEAN: `bool` | boolean: `bool` | BOOLEAN: `bool` | BOOLEAN: `bool` | bool: `bool` | BOOLEAN: `bool` |
| integer |  | TINYINT: `int64`<br>SMALLINT: `int64`<br>INTEGER: `int64`<br>BIGINT: `int64`<br>UNSIGNED_INT: `int64`<br>UNSIGNED_LONG: `int64` |  | tinyint: `int64`<br>smallint: `int64`<br>int: `int64`<br>bigint: `int64`<br>uint8: `int64`<br>uint16: `int64`<br>uint32: `int64` | BIGINT: `int64`<br>INTEGER: `int64` | integer: `int64` | INTEGER: `int64` | integer: `int64` | INT: `int64`<br>LONG: `int64` | INTEGER: `int64` | int: `int64` | TINYINT: `int64`<br>SMALLINT: `int64`<br>INTEGER: `int64`<br>BIGINT: `int64` |
| unsigned integer |  |  |  | uint64: `uint64` |  | unsigned integer: `uint64` |  |  |  |  |  |  |
| number | integer: `int64`<br>double: `float64` |  | number: `int64` |  |  |  | NUMERIC: `int64 for an integer` |  |  | NUMERIC: `int64 for a number with no fraction that fits` |  |  |
| float |  | DOUBLE: `float64`<br>FLOAT: `float64` |  | float: `float64`<br>double: `float64` | FLOAT: `float64`<br>REAL: `float64`<br>DOUBLE: `float64` | float: `float64` | REAL: `float64` | float: `float64` | FLOAT: `float64`<br>DOUBLE: `float64` | REAL: `float64` | float: `float64` | REAL: `float64`<br>DOUBLE: `float64` |
| decimal |  | DECIMAL: `*apd.Decimal` |  | decimal: `*apd.Decimal` | DECIMAL: `float64` |  |  |  | BIG_DECIMAL: `*apd.Decimal` |  | decimal: `*apd.Decimal` | DECIMAL: `*apd.Decimal`<br>trino 483 NUMBER: `*apd.Decimal` |
| big integer |  |  |  |  |  |  |  |  |  |  |  |  |
| string | string: `string` | CHAR: `string`<br>VARCHAR: `string` | string: `string` | string: `string` | VARCHAR: `string`<br>CHAR: `string` | string: `string`<br>tag: `string` | TEXT: `string` | string: `string` | STRING: `string` | TEXT: `string` | string: `string` | CHAR: `string`<br>VARCHAR: `string` |
| binary |  | BINARY: `[]byte`<br>VARBINARY: `[]byte` | bytes as a base64 string: `[]byte` | binary: `[]byte` | COMPLEX<HLLSketch>: `[]byte` |  | BLOB: `[]byte` | byte array: `[]byte` | BYTES: `[]byte` | BLOB: `[]byte` | bytes: `[]byte` | VARBINARY: `[]byte`<br>HYPERLOGLOG: `[]byte`<br>P4HYPERLOGLOG: `[]byte`<br>SETDIGEST: `[]byte`<br>QDIGEST: `[]byte`<br>TDIGEST: `[]byte` |
| date |  | DATE: `dbimp.Date` |  | date: `dbimp.Date` | DATE: `dbimp.Date` |  | DATE: `dbimp.Date` | date: `dbimp.Date` |  | DATE: `dbimp.Date` |  | DATE: `dbimp.Date` |
| time of day |  | TIME: `dbimp.LocalTime` |  |  |  |  |  | local time: `dbimp.LocalTime` |  |  |  | TIME: `dbimp.LocalTime` |
| time of day with offset |  |  |  |  |  |  |  | zoned time: `dbimp.OffsetTime` |  |  |  | TIME WITH TIME ZONE: `dbimp.OffsetTime` |
| local timestamp |  | TIMESTAMP: `dbimp.LocalDateTime` |  |  |  |  | DATETIME: `dbimp.LocalDateTime` | local datetime: `dbimp.LocalDateTime` |  | DATETIME: `dbimp.LocalDateTime` |  | TIMESTAMP: `dbimp.LocalDateTime` |
| timestamp |  |  |  | timestamp: `time.Time`<br>timestamp_tz: `time.Time` | TIMESTAMP: `time.Time` | timestamp: `time.Time` | TIMESTAMP: `time.Time` | offset datetime: `time.Time`<br>zoned datetime: `time.Time` | TIMESTAMP: `time.Time` | TIMESTAMP: `time.Time` | datetime: `time.Time` | TIMESTAMP WITH TIME ZONE: `time.Time` |
| duration |  |  |  |  |  |  |  |  |  |  | duration: `time.Duration` |  |
| interval |  | INTERVAL: `dbimp.Interval` |  | interval: `dbimp.Interval` |  |  |  | duration: `dbimp.Interval` |  |  |  | INTERVAL YEAR TO MONTH: `dbimp.Interval`<br>INTERVAL DAY TO SECOND: `dbimp.Interval` |
| uuid |  | UUID: `uuid.UUID` |  |  |  |  |  | uuid: `uuid.UUID` |  |  | uuid: `uuid.UUID` | UUID: `uuid.UUID` |
| ip address |  |  |  |  |  |  |  |  |  |  |  |  |
| json |  |  |  | variant: `the decoded JSON value` | COMPLEX<json>: `the decoded JSON value` |  |  |  | JSON: `the decoded JSON value` |  |  | JSON: `the decoded JSON value`<br>trino 483 VARIANT: `the decoded JSON value` |
| array | array: `[]any` | ARRAY: `[]any` | array: `[]any` | array: `[]any` | ARRAY: `[]any` |  |  | list: `[]any` | INT_ARRAY: `[]any`<br>LONG_ARRAY: `[]any`<br>FLOAT_ARRAY: `[]any`<br>DOUBLE_ARRAY: `[]any`<br>BOOLEAN_ARRAY: `[]any`<br>STRING_ARRAY: `[]any`<br>TIMESTAMP_ARRAY: `[]any` |  | array: `[]any` | ARRAY: `[]any` |
| set |  |  |  |  |  |  |  |  |  |  | set: `[]any` |  |
| map | object: `map[string]any` |  | object: `map[string]any` | map: `map[string]any` |  |  |  | map: `map[string]any` | MAP: `map[string]any` |  | object: `map[string]any` | MAP: `map[string]any` |
| tuple |  |  |  | tuple: `[]any` |  |  |  |  |  |  |  | ROW: `[]any` |
| vector |  |  |  | vector: `dbimp.Vector[float32]` |  |  | F32_BLOB: `dbimp.Vector[float32]` | vector: `dbimp.Vector of the Go type of its coordinates` |  |  |  |  |
| geometry |  |  |  | geometry: `string`<br>geography: `string` |  |  |  | point: `neo4j.Point` |  |  | geometry: `map[string]any of GeoJSON` | GEOMETRY: `string`<br>SPHERICALGEOGRAPHY: `string` |
| range |  |  |  |  |  |  |  |  |  |  | range: `string` |  |
| record id |  |  |  |  |  |  |  |  |  |  | record: `RecordID` |  |
| node |  |  |  |  |  |  |  | node: `neo4j.Node` |  |  |  |  |
| relationship |  |  |  |  |  |  |  | relationship: `neo4j.Relationship` |  |  |  |  |
| path |  |  |  |  |  |  |  | path: `neo4j.Path` |  |  |  |  |
| bitmap |  |  |  | bitmap: `none` |  |  |  |  |  |  |  |  |
| other |  |  |  |  |  |  |  |  |  |  | table: `string` | IPADDRESS: `string` |
<!-- /dbimp:matrix -->

## What W21 changed

D138 decided four types of the root package and two rules, D139 added two
types, and W21 changed the drivers to follow them on 2026-09-30:

- A date is a `dbimp.Date` in Databend, InfluxDB and Neo4j, where it was a
  `time.Time` at midnight in UTC in the first two, and a `neo4j.Date`.
- A time of day is a `dbimp.LocalTime` in InfluxDB and Neo4j, where it was
  the text of the server, and a `neo4j.LocalTime`.
- A timestamp with no zone is a `dbimp.LocalDateTime` in Neo4j, where it was
  a `neo4j.LocalDateTime`.
- An interval is a `dbimp.Interval` in Databend, InfluxDB and Neo4j, where
  it was the text of the server, and a `neo4j.Duration`.
- An unsigned integer of 64 bits is a `uint64` in Databend, as in InfluxDB,
  where it was an `int64`, or an `*apd.Decimal` above the range of `int64`.
- A time of day with an offset is a `dbimp.OffsetTime` in Neo4j, where it
  was a `neo4j.Time` (D139).
- A vector is a `dbimp.Vector[T]` in Neo4j and Databend, where it was a
  `neo4j.Vector` and a `[]float32` (D139). A caller sends the type that it
  read, and a plain slice goes as a list.

A geometry stays a map of GeoJSON in SurrealDB, a `neo4j.Point` in Neo4j,
and WKT in Databend (D136), because the databases differ in what a geometry
holds.

The InfluxDB date, time of day and interval are not in its type table,
because its survey names no such type. [INFLUXDB.md](INFLUXDB.md) describes
them.

## How a type is mapped

Step 8a of [DRIVER.md](DRIVER.md) maps each type of a new driver, after the
survey and the measurements, and before its decisions:

1. List each type that the survey marks `yes`, with its name on the wire and
   what step 6 recorded for it.
2. Give each type a kind from the list above, and the Go type of that kind.
   A type that fits no kind, or whose Go type differs from the Go type of its
   kind, needs a reason, and a decision of step 9 names it.
3. Ask at least two models to review the mapping, and record what each one
   said in the product document.
4. Write the mapping as the type table of the product document, in the form
   that `dbimptest.TypeTable` writes, with the column Kind.
5. Ken reviews the mapping before any code is written. Step 10 then
   generates the table from the code, and the two must agree.

## Second opinions

Gemini and DeepSeek reviewed the kinds and the shared types on 2026-09-30,
from a brief with the mappings of the seven drivers.

- Both proposed a shared type for a date, a time of day, a timestamp with no
  zone and an interval with months, days and nanoseconds, because Go has no
  type for any of them, and `time.Time` gives each one a zone that it does
  not have.
- Both said that a node, a relationship and a path of a graph stay types of
  the driver, because graph databases differ in what they hold.
- Gemini said that an unsigned integer of 64 bits is always a `uint64` when
  the database names the type. DeepSeek said that its Go type follows its
  value. D135 follows the type, so Gemini's rule fits it.
- Gemini said that a geometry keeps the form of its driver, and DeepSeek
  proposed a shared type with a type, an SRID and coordinates.
- Gemini proposed a shared type for a range and for a record id. DeepSeek
  kept a range as text and a record id as a type of the driver.
- Both proposed a UUID of their own, as `[16]byte`. Go 1.27 has `uuid.UUID`,
  which D25 uses, so the proposal does not apply.

Ken then asked why a date, a time of day and a timestamp with no zone
cannot all be a `time.Time`, read with `time.ParseInLocation` and
`time.Local`. Both models kept the types of the root package. They said
that a `time.Time` cannot show in `*any` which of the three kinds a value
is, that `time.Local` depends on the zone of the process and breaks hard
rule 2, and that a local midnight does not exist on some days in a zone
that changes its clock at midnight. The source of the drivers for
PostgreSQL showed the other side: lib/pq v1.12.3 returns a date and a
timestamp with no zone as a `time.Time`, and a time of day as a `time.Time`
on 0000-01-01 in UTC. Ken chose the types of D138.

Ken did not like the name `TimeOfDay`. Asked for a better one, both models
chose `LocalTime`, with `Time` as the second choice, because it pairs with
`LocalDateTime` and is the name of java.time and of Neo4j.

For the interval, the source of the drivers for PostgreSQL showed what they
do: lib/pq v1.12.3 returns its text as a `[]byte`, and pgx v5.11.0 returns
its text as a `string` through `database/sql`, and a `pgtype.Interval` of
months, days and microseconds through its own API. Ken chose a struct of
that shape, with nanoseconds.

Ken asked whether `LocalTimestamp` is a better name than `LocalDateTime`.
Both models kept `LocalDateTime`, because a Go reader takes a timestamp for
an instant, and because java.time, JDBC 4.2, Neo4j and
`cloud.google.com/go/civil` name the type after a date and a time. The SQL
types and the drivers that mirror them, such as `pgtype.Timestamp`, name it
a timestamp.

Ken then asked whether to import `cloud.google.com/go/civil`, and to name
its types with aliases. Both models advised against it, because `civil` has
no interval, and because it is a package of the root module
`cloud.google.com/go`, whose maintainers are the team of the Google Cloud
libraries, and which is still at v0. A test in a copy of dbimp measured the
cost: one line in `go.mod` and four in `go.sum` of dbimp, and one line in
the `go.mod` of a consumer, whose graph of modules then holds 55 modules,
gRPC and OpenTelemetry among them, though none of them is built. Ken chose
to write the four types in the root package.

## Open questions

None. W21 carries out D138.
