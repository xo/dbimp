# InfluxDB

This file holds what is known about InfluxDB, for the driver that D73 names
first after Neo4j. W11 in [BACKLOG.md](BACKLOG.md) is the work, and it
follows [DRIVER.md](DRIVER.md). The headings are the template of that file.

Step 6 measured the servers on 2026-09-28, on seven releases that `dbrun`
started: `influxdb-1.11.8`, `influxdb-1.13.1`, `influxdb-2.8.0`,
`influxdb-2.9.1`, `influxdb-3.9.13`, `influxdb-3.10.6` and
`influxdb-3.11.5`. A fact marked "measured" names its releases. Each
recorded exchange is a file under `testdata/influxdb/`, named for its
release and a number, so "3.11.5-042" is
`influxdb-3.11.5-042-post--api-v3-query-sql.json`. The requests are in
`testdata/influxdb/requests.json`.

A fact that no server showed is "not measured", and names its source. The
sources, each read on 2026-09-28, are these:

- "The Core API reference" is
  `api-docs/influxdb3/core/influxdb3-core-openapi.yaml` in
  `github.com/influxdata/docs-v2`. It names the version v3.11.2.
- "The parameter guide" is
  `content/shared/influxdb3-query-guides/sql/parameterized-queries.md`, and
  "the SQL reference" is `content/shared/sql-reference/`, both in `docs-v2`.
- "The token guides" are `content/shared/influxdb3-admin/tokens/` and
  `content/influxdb3/enterprise/admin/tokens/resource/` in `docs-v2`.
- "The configuration reference" is
  `content/shared/influxdb3-cli/config-options.md` in `docs-v2`.
- "The Enterprise guides" are `content/influxdb3/enterprise/admin/license.md`
  and `delete-data.md` in `docs-v2`.
- "The v1 API reference" is `api-docs/influxdb/v1/influxdb-oss-v1-openapi.yaml`
  in `docs-v2`, and "the v1 guides" are
  `content/influxdb/v1/administration/config.md` and
  `authentication_and_authorization.md` in `docs-v2`.
- "The v2 API reference" is
  `api-docs/influxdb/v2/influxdb-oss-v2-openapi.yaml`, and "the Flux note"
  is `content/flux/v0/future-of-flux.md`, both in `docs-v2`.
- "The source" is `github.com/influxdata/influxdb` at the tag `v3.11.5`.
- "The dbmeta entry" is `container/influxdb.go` in `dbmeta`, staged for
  dbmeta D112 and dbmeta D114 and not committed.
- "The Go client" is `github.com/InfluxCommunity/influxdb3-go/v2` v2.17.0.
- "Gemini" is `gemini-3.1-pro-preview`, and "DeepSeek" is `deepseek-flash`,
  both asked on 2026-09-28.

## Summary

- InfluxDB is a time series database from InfluxData. Its major versions
  speak different languages:
  - InfluxDB 1 speaks InfluxQL at `/query` (measured on 1.11.8 and 1.13.1).
  - InfluxDB 2 speaks Flux at `/api/v2/query`, and InfluxQL at `/query`
    through its v1 compatibility API (measured for InfluxQL on 2.8.0 and
    2.9.1, and from the v2 API reference for Flux).
  - InfluxDB 3 Core speaks SQL at `/api/v3/query_sql` and InfluxQL at
    `/query` (measured on 3.9.13, 3.10.6 and 3.11.5). The SQL is the SQL of
    Apache DataFusion 51.0.0 on 3.9.13 and 3.11.5 (measured, 3.11.5-053).
- InfluxDB 3 also takes SQL and InfluxQL through Flight SQL over gRPC (the
  Core API reference). The Go client queries only through Flight.
- Flux is in maintenance mode, and InfluxDB 3 does not have it (the Flux
  note).
- The releases, and their names in `dbrun`, are in D79. `dbrun` starts each
  one from the dbmeta entry. The Tested tier is 1.13.1, 2.9.1, 3.9.13 and
  3.11.5, and the Nightly tier is 1.11.8, 2.8.0 and 3.10.6.
- R, H and S:
  - R: InfluxDB 1, 2 and 3 Core pass, because `dbrun` starts every release
    above (measured). InfluxDB 3 Enterprise needs a person to follow a link
    in an email before it starts, so it fails R (the Enterprise guides).
  - H: every release takes queries over HTTP (measured).
  - S: SQL and InfluxQL meet S. Flux meets S too (D75), and the driver does
    not speak it (D78).
  - The priority is P1 (TARGETS.md).
- `dburl` has two schemes for this driver (dburl D29). The scheme
  `influxdb` has the dialect `influxdb`, the aliases `in` and `influx`, and
  the generator `GenInfluxDB`. The scheme `influxql` has the dialect
  `influxql`, the alias `iq`, and the generator `GenInfluxQL`, which names
  `influxdb` as the Go driver and adds `sqlmode=disable`. Both name the
  `GoPackage` `github.com/xo/dbimp/influxdb`.
- `usql` has no driver for InfluxDB (`usql/docs/BACKLOG.md`).
- The driver is `influxdb`, with the dialects `influxdb` and `influxql`
  (D78). D80 to D83 settle the rest of step 9, and D96 names the first
  column of InfluxQL.

## Requests

- A SQL statement is `POST /api/v3/query_sql` with a JSON body of `db`, `q`,
  `format` and `params` (measured on 3.9.13 and 3.11.5). `GET` with the same
  keys in the query string works too (measured on 3.11.5). The driver sends
  `"format": "json"`.
- An InfluxQL statement is `POST /query`, with `db` in the query string and
  the form `q=...` in the body, as
  `application/x-www-form-urlencoded` (measured on every release). `GET` with
  `q` in the query string works for `SHOW` and `SELECT` (measured on every
  release, 1.13.1-007).
- `/api/v3/query_sql` is HTTP 404 on InfluxDB 1 (measured, 1.13.1-008). On
  InfluxDB 2 it is HTTP 200 with the HTML page of the user interface
  (measured, 2.9.1-008). So a SQL request to InfluxDB 2 cannot be read as
  JSON.
- HTTP Basic authentication works on every endpoint and every release
  (measured):
  - InfluxDB 1 takes a user and its password.
  - InfluxDB 2 takes a v1 user and its password, or any user with the token
    as the password. `Authorization: Token <token>` works too.
  - InfluxDB 3 takes any user with the token as the password.
    `Authorization: Bearer <token>` works too.
- A token in the header `Authorization`, as `auth=bearer` sends it
  (measured by hand on 2026-09-29, D116):
  - InfluxDB 1.13.1 answers `Bearer` with "bearer auth disabled", because
    it takes a JWT only with a shared secret configured, and cannot parse
    `Token`.
  - InfluxDB 2.9.1 refuses `Bearer`, and takes `Token`.
  - InfluxDB 3.11.5 takes both, for InfluxQL, SQL and the ping.
- `GET /ping` answers HTTP 204 without credentials on InfluxDB 1 and 2, even
  with a wrong password (measured, 1.13.1-023 and 2.9.1-023). On InfluxDB 3
  it answers HTTP 401 without a token (measured, 3.11.5-048).
- `dbrun` gives each release a database `dbmeta`. On InfluxDB 2, `/query`
  maps `db=dbmeta` to the bucket through the mapping that the dbmeta entry
  makes, with the retention policy `autogen`.
- Writes are line protocol, at `/write` on every release, and also at
  `/api/v3/write_lp` on InfluxDB 3 (measured). No server parses INSERT. The
  driver takes `INSERT [INTO <database>[.<retention-policy>]] <line
  protocol>`, as the `influx` shell does, in both dialects, and sends the
  lines to `/write` (D85). It binds each argument as a literal of line
  protocol (measured through the driver on every release, 2026-09-28).

## The DSN

D82 settles the DSN. It is `influxdb://user:password@host:port/database`,
with these keys:

| Key | Values | Default | Decision |
| --- | --- | --- | --- |
| `sqlmode` | `disable`, `allow`, `prefer`, `require` | `prefer` | D78 |
| `version` | `1`, `2`, `3` | `3` | D78 |
| `describe` | `always`, `disable` | `always` | D80 |
| `chunked` | `prefer`, `disable` | `prefer` | D83 |
| `rp` | a retention policy | none | D82 |
| `tls` | `true`, `false` | `false` | D82 |
| `auth` | `basic`, `bearer` | `basic` | D94 and D116 |

- The path is the database, which the driver sends as `db`.
- Each key that can change for one statement is also an option (D109). The
  path is `WithDatabase`, `rp` is `WithRetentionPolicy`, `chunked` is
  `WithChunked`, and `describe` is `WithDescribe`. An INSERT without `INTO`
  writes to the database and the retention policy of the options.
- A token is the password. Without a port, the driver uses 8086 for `version`
  `1` or `2`, and 8181 otherwise.
- Examples: `influxdb://_admin:apiv3_token@localhost:8181/dbmeta` for
  InfluxDB 3, and
  `influxdb://admin:secret@localhost:8086/dbmeta?sqlmode=disable&version=1`
  for InfluxDB 1.
- From dbmeta `415e830`, the `url` of each principal is the DSN of D82, such
  as `influxdb://_admin:<token>@127.0.0.1:<port>/dbmeta`, and the tests use
  it as `dbrun` prints it.

## Responses

### SQL on InfluxDB 3

- `json` is one JSON array with one object for each row, such as
  `[{"time":"2023-11-14T22:13:20","s":"text","f":1.5}]` (measured on 3.9.13
  and 3.11.5, 3.11.5-014). It has no list of columns and no types.
- The keys keep the order of the statement. `SELECT *` gives the columns of
  the table in order by name (measured, 3.11.5-014 and 3.11.5-016).
- A NULL is not written, so the key is missing from its object.
  `SELECT NULL AS a, 1 AS b` returns `[{"b":1}]` (measured, 3.11.5-012). A
  row of NULLs is `{}` (measured, 3.11.5-064). A key can arrive first in a
  later row.
- A result with no rows is `[]` (measured, 3.11.5-018).
- `DESCRIBE` followed by a `SELECT` returns one object for each column, with
  `column_name`, `data_type` and `is_nullable`, in the order of the
  statement, even for a result with no rows (measured, 3.11.5-011, 013 and
  017). It refuses every statement that is not a `SELECT`: HTTP 400 on
  3.9.13 and HTTP 405 on 3.11.5 (measured, 3.9.13-090 and 3.11.5-090). D80
  says how the driver uses it.
- The body streams with `Transfer-Encoding: chunked` (measured). There is no
  paging and no cap: 12,000 rows arrive in one body (measured, 3.11.5-037).
  The query guide and the Core API reference name no cursor.
- The other formats are `jsonl`, `csv`, `pretty` and `parquet` (measured, by
  the error for an unknown format). `csv` writes nothing for a result with
  no rows, and writes a NULL and an empty string alike (measured on
  3.11.5). An `Accept` header of `application/vnd.apache.arrow.stream` is
  HTTP 400 (measured on 3.11.5).
- The server does not compress the answer, even with
  `Accept-Encoding: gzip` (measured, 3.11.5-064).

### InfluxQL on every release

- The answer is `{"results": [...]}`, with one result for each statement.
  A result holds `statement_id`, and then `series`, `error`, or neither. A
  series holds `name`, an optional `tags` object, `columns` and `values`,
  one array for each row, with `null` for a NULL (measured on every
  release). So the columns arrive before the rows, and rule 1 of D18
  applies. D81 maps each series to a result set, and D96 names its first
  column `measurement`.
- `GROUP BY host` gives one series for each value of the tag, with the tag
  in `tags` and not in `columns` (measured, 1.13.1-010). A statement on two
  measurements gives one series for each, with different columns (measured,
  1.13.1-011).
- A statement with no series is `{"statement_id":0}` (measured on every
  release, 1.13.1-012).
- With `chunked=true`, InfluxDB 1 sends one JSON document for each chunk of
  10,000 rows, and a series that continues holds `"partial":true` (measured,
  1.13.1-017 and 1.13.1-018). InfluxDB 2 sends its chunks with a
  `Content-Length`, so it builds the whole answer first (measured on 2.8.0
  and 2.9.1). InfluxDB 3 leaves out the result of each statement that has
  no series (measured, 3.11.5-059). D83 settles which releases the driver
  asks for chunks.
- InfluxDB 2 leaves out the result of `DELETE` and of `DROP MEASUREMENT`, in
  both forms (measured, 2.9.1-031). D83 fills the gap.
- A result without `chunked` is not capped: 20,000 rows arrive in one
  document on 1.13.1, 2.9.1 and 3.11.5 (measured by `curl`, not recorded).
  `max-row-limit` caps a result on InfluxDB 1, and its default is 0, which
  is no limit (the v1 guides).
- InfluxDB 1 compresses the answer with `Accept-Encoding: gzip` (measured,
  1.13.1-033). InfluxDB 2 and 3 do not (measured, 2.9.1-033 and
  3.11.5-063).
- No recorded answer is a redirect (measured on every release).

## Types

The type table is written from the code by `TestTables` (step 10). It has
one row for each type that a line of line protocol writes, which are the
types of `features.json`.

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| null | null | `nil in SQL, from the data_type Null. InfluxQL: nil` | `interface {}` | `NULL` | yes |
| float | float | `float64 in SQL, from the data_type Float64. InfluxQL: float64, or int64 for a whole number` | `float64` | `FLOAT64` | yes |
| integer | integer | `int64 in SQL, from the data_type Int64. InfluxQL: int64` | `int64` | `INT64` | yes |
| unsigned integer | unsigned integer | `uint64 in SQL, from the data_type UInt64. InfluxQL: int64, or uint64 above the range of int64` | `uint64` | `UINT64` | yes |
| string | string | `string in SQL, from the data_type Utf8. InfluxQL: string` | `string` | `UTF8` | yes |
| boolean | boolean | `bool in SQL, from the data_type Boolean. InfluxQL: bool` | `bool` | `BOOLEAN` | yes |
| tag | string | `string in SQL, from the data_type Dictionary(Int32, Utf8). InfluxQL: string` | `string` | `DICTIONARY(INT32, UTF8)` | yes |
| timestamp | timestamp | `time.Time in SQL, from the data_type Timestamp(ns). InfluxQL: time.Time, in the column time` | `time.Time` | `TIMESTAMP(NS)` | no |
<!-- /dbimp:types -->

SQL can make other types in an expression, and the driver decodes each one
by its `data_type`: a decimal as an `*apd.Decimal`, a `Date32` or a
`Date64` as a `dbimp.Date`, a `Time32` or a `Time64` as a `dbimp.LocalTime`,
an `Interval` as a `dbimp.Interval` (D138), binary data as a `[]byte`, a
`Duration` as a `time.Duration`, and a list or a struct as it decodes JSON.
An argument of the types of D138 is the text that the server casts: ISO 8601
for a date, a time of day and a date and time, and the form of the server for
an interval, because the server takes no interval in ISO 8601.
These are the facts that step 6 measured.

SQL on InfluxDB 3, with the `data_type` that `DESCRIBE` gives and the JSON of
a value (measured on 3.9.13 and 3.11.5, 3.11.5-023 to 026):

| `data_type` | JSON |
| --- | --- |
| `Int64`, `Int8` | a number, such as `9223372036854775807` |
| `UInt64`, `UInt8` | a number, such as `18446744073709551615` |
| `Float64`, `Float32` | a number, such as `1.5` or `0.0`. NaN, +Inf and -Inf are an explicit `null` |
| `Decimal128(38, 9)` | a number with every digit, such as `12345678901234567890.123456789` |
| `Utf8` | a string |
| `Dictionary(Int32, Utf8)` | a string. A tag is this type |
| `Boolean` | `true` or `false` |
| `Timestamp(ns)` | a string with no zone, such as `"2024-01-02T03:04:05.123456789"` |
| `Timestamp(ns, "UTC")` | a string with `Z` |
| `Date32` | a string, such as `"2024-01-02"` |
| `Date64` | a string with a time, such as `"2026-09-30T00:00:00"` (measured on 3.11.5 on 2026-09-30) |
| `Time64(ns)` | a string, such as `"12:30:00"`, or `"12:30:00.500"` with a fraction, as `Time32(ms)` writes it too (measured on 3.11.5 on 2026-09-30) |
| `Interval(MonthDayNano)` | a string, such as `"1 days 2 hours"` or `"14 mons 3 days 4 hours 5 mins 6.500000000 secs"`, with a sign on each part, and `""` for zero (measured on 3.11.5 on 2026-09-30) |
| `Duration(s)`, `Duration(ms)`, `Duration(ns)` | a string in ISO 8601, such as `"PT1.5S"` or `"-PT2.5S"`, and `"P0D"` for zero (measured on 3.11.5 on 2026-09-30) |
| `Binary` | a string of hex, such as `"6162"` |
| `List(Int64)` | an array |
| `Struct("a": Int64)` | an object |
| `Null` | always a missing key |

On 3.11.5, on 2026-09-30, the server cast no interval to `Interval(YearMonth)`
or `Interval(DayTime)`, so neither form is recorded. It refused an interval
in ISO 8601, such as `P1M2DT3.5S`, and cast its own form, such as `14 mons -3
days -0.000001 secs`. A date given as a parameter, `CAST($a AS DATE)`, wrote
back as the date, and compared as false with the same date as a literal, which
is a fault of the server.

- A NaN, +Inf or -Inf reaches the JSON as an explicit `null`, and a NULL
  leaves out its key (measured, 3.11.5-024, and on 3.11.5 on 2026-09-29).
  So the driver tells a value that is not finite from a NULL, and reads it
  as `math.NaN()`, because the JSON cannot tell the three apart (D80).
  `format=csv` writes them as `NaN`, `inf` and `-inf`. Line protocol refuses
  `NaN`, so only an expression makes one, and InfluxQL answers 0 for a
  division by zero.
- A decimal keeps every digit only when it is made from a string. A literal
  such as `CAST(12345678901234567890.123456789 AS DECIMAL(38,9))` passes
  through a float first and loses digits (measured by `curl` on 3.11.5).
- A parameter is typed `Null` by `DESCRIBE`, whatever its value (measured,
  3.11.5-030). So a column typed `Null` can hold values.
- A line protocol field is a float, an integer, an unsigned integer, a
  string or a boolean, and a tag is a string (measured). InfluxDB 1 refuses
  an unsigned integer, with HTTP 400 and `invalid number` (measured,
  1.13.1-003).

InfluxQL on every release:

- A float and an integer are both JSON numbers. A float that holds 0 arrives
  as `0` (measured, 1.13.1-014). `/query` names no types. `SHOW FIELD KEYS`
  gives the type of each field (measured, 1.13.1-015). D83 decodes a number
  by its text.
- The column `time` is a string in RFC 3339 with `Z`, such as
  `"2023-11-14T22:13:20Z"`, or with the offset of a `tz()` clause, such as
  `"2023-11-15T07:13:20+09:00"` (measured, 3.11.5-095). With `epoch=ms` it
  is a number (measured, 3.11.5-096). The driver sends no `epoch`.
- The largest integers, `9223372036854775807` and `-9223372036854775808`,
  and the unsigned `18446744073709551615`, arrive exactly (measured,
  2.9.1-013).
- An empty string and a NULL are different: `""` and `null` (measured,
  1.13.1-013).

## Parameters

- SQL takes named parameters, `$name`, and positional ones, `$1`, from the
  object `params` (measured, 3.11.5-031 and 033). The key of `$1` is `"1"`.
  The key `"$1"` is not found (measured, 3.11.5-116).
- A parameter works in `SELECT`, in `WHERE` and in `LIMIT` (measured,
  3.11.5-031 and 3.11.5-117). The parameter guide says that it works only in
  `WHERE`, and the server disagrees.
- A value is null, a boolean, a number or a string. An array or an object is
  HTTP 400 (measured, 3.11.5-114 and 115). A time is a string (measured,
  3.11.5-113).
- A missing parameter is HTTP 400 for the statement, and `DESCRIBE` of the
  same statement succeeds (measured, 3.11.5-034 and 035).
- InfluxQL takes named parameters from the form value `params`, JSON text,
  on every release (measured, 1.13.1-016). A time works as a string on every
  release (measured, 3.11.5-119). A time as an integer works on 1.13.1 and
  2.9.1, and is an error on 3.11.5 (measured). An array is an error on every
  release (measured). `$1` takes the key `"1"` of `params` in InfluxQL, on
  1.13.1, 2.9.1 and 3.11.5, as a name does (measured by hand on
  2026-09-29, D111).
- So the server binds every parameter, and the driver needs no parser for
  placeholders (D34). `INSERT` is the exception: the driver writes each
  argument into the line protocol itself (D85).

## Transactions

- No release has transactions (not measured, because no statement or
  endpoint begins one to send). The documents, Gemini and DeepSeek name
  none.
- SQL has no `INSERT`, `UPDATE` or `DELETE`. Each is HTTP 400 with
  `DML not supported` (measured, 3.11.5-067, 068 and 071). The driver takes
  INSERT of line protocol itself (D85), and sends DELETE to the server,
  which runs it on InfluxDB 1 and 2 and refuses it on InfluxDB 3 Core
  (measured on 3.11.5, with a WHERE clause too).
- A write of the same series and the same time replaces the value of each
  field that it names, and keeps each field that it leaves out, so a point
  cannot be updated to NULL (measured by `TestIntegrationRoundTrip` on every
  release). A new tag value or a new time is a new point.
- InfluxQL has `DELETE`, `DROP SERIES` and `DROP MEASUREMENT` on InfluxDB 1
  (measured, 1.13.1-037 to 039). InfluxDB 2 runs `DELETE` and
  `DROP MEASUREMENT`, and answers `not implemented` to `DROP SERIES`
  (measured, 2.9.1-037 to 039). InfluxDB 3 answers `not implemented` to each
  (measured, 3.11.5-070 to 073). No release has `UPDATE` (measured).

## Errors

SQL on InfluxDB 3:

- A plan error is HTTP 400, such as a missing parameter or a statement that
  DataFusion does not plan (measured, 3.11.5-035).
- A column that does not exist is HTTP 500 with `Schema error: ...`
  (measured, 3.11.5-041).
- A feature that DataFusion does not implement is HTTP 400 on 3.9.13 and
  HTTP 405 on 3.11.5 (measured, 3.9.13-055 and 3.11.5-055).
- A database that does not exist is HTTP 404 with
  `{"error":"query error: database not found: nope"}` (measured by `curl` on
  3.11.5).
- A failed authentication is HTTP 401 with
  `{"error": "the request was not authenticated"}` (measured, 3.11.5-050).
- An error body is plain text or JSON (measured).
- An error after some rows closes the body before its end, with HTTP 200 and
  no text of the error (measured, 3.9.13-042 and 3.11.5-042). The body holds
  the first batches of rows and no closing `]`. So the driver reads
  `io.ErrUnexpectedEOF` and returns it from `Rows.Next` (D8).

InfluxQL:

- A statement that does not parse is HTTP 400 on InfluxDB 1 and 2, and
  HTTP 200 with the error in its result on InfluxDB 3 (measured,
  1.13.1-019, 2.9.1-019 and 3.11.5-043).
- An error in a later statement is HTTP 200, with the error in the result of
  that statement, after the rows of the statements before it (measured,
  1.13.1-020). On InfluxDB 3, a failed statement does not stop the
  statements after it (measured, 3.11.5-061).
- A failed authentication is HTTP 401 on every release (measured,
  1.13.1-024). The bodies differ: `{"error":"authorization failed"}` on 1,
  `{"code":"unauthorized","message":"Unauthorized"}` on 2.
- A refused privilege is HTTP 403 on InfluxDB 1, and HTTP 200 with
  `insufficient permissions` in the result on InfluxDB 2 (measured,
  1.13.1-080 and 2.9.1-080).
- A database that does not exist is HTTP 200 with the error in the result
  on InfluxDB 3 (measured by `curl` on 3.11.5).
- No recorded answer is HTTP 429, and no header limits the rate (measured
  on every release).

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose
`HTTPStatus` is 401. Every release sends HTTP 401 for a wrong password or
token (recorded: "a wrong password (influxql)", "a wrong password (sql)" and
"ping with a wrong password"), with the bodies that this section lists.

It is false for these refusals, because the credential was good and the
principal lacks a permission:

- HTTP 403 on InfluxDB 1 for a statement or a write that the user cannot run
  (measured, 1.11.8-074 and 078).
- HTTP 200 with `insufficient permissions` in the result on InfluxDB 2
  (measured, 2.9.1-080).

It is also false for every other status, such as HTTP 400. The test
`TestAuthenticationInfluxdb` reads each of these answers from `testdata/influxdb/`.

## Cancellation and timeouts

- On InfluxDB 3, a query that the client leaves is recorded in
  `system.queries` with the phase `cancel` (measured, 3.11.5-045). So the
  server stops a query when the client disconnects.
- InfluxDB 3 has no statement or endpoint to stop a query, and sends no
  header that names the id of a query (measured, 3.11.5-118).
- InfluxDB 1 has `SHOW QUERIES` and `KILL QUERY`, and `KILL QUERY` needs the
  administrator (measured, 1.13.1-021, 022 and 077). InfluxDB 2 answers
  `not implemented` to both (measured, 2.9.1-021). InfluxDB 3 does not parse
  them (measured, 3.11.5-046).
- InfluxDB 1 and 2 stop a query when the client disconnects (measured by
  hand on 2026-09-29). On 1.13.1, a query that took 3.3 seconds in full was
  gone from `SHOW QUERIES` half a second after the client left at 1 second,
  with and without chunks. On 2.9.1, which has no `SHOW QUERIES`, the same
  query executed for 1.8 seconds in full and for 1.0 second when the
  client left, by `influxql_service_executing_duration_seconds` of
  `/metrics`. D115 relies on this. `query-timeout` on InfluxDB 1 stops a
  query after a time, and its default is `0s`, which is no limit (the v1
  guides). It holds for the whole server. The manual of InfluxDB 3 names no
  timeout for one query in `/api/v3/query_sql` or `/query` (read on
  2026-09-29). So `WithTimeout` gives `dbimp.ErrNotSupported` (D109).
- `GET /query` runs a write on 1.13.1, with only the warning "deprecated
  use of 'CREATE DATABASE ...' in a read only context, please use a POST
  request instead". 2.9.1 and 3.11.5 answer `not implemented` to the same
  statement over GET and POST alike (measured by hand on 2026-09-29). No
  release has another read-only mode, so `WithReadonly` gives
  `dbimp.ErrNotSupported` (D109).
- A query with `GROUP BY time()` and no bound on `time` counts its groups
  from 1970, and a group of one second made 1.13.1 stop with no answer
  (measured by hand on 2026-09-29). With a bound on `time`, the same query
  ran.

## Statements

- SQL takes one statement. Two statements are HTTP 400 on 3.9.13 and HTTP
  405 on 3.11.5 (measured, 3.9.13-055 and 3.11.5-055).
- InfluxQL takes statements separated by `;`, and answers each one in order
  with its `statement_id` (measured on every release, 1.13.1-028). D83 fills
  a gap in the ids.
- A comment with `--` and `/* */` works in SQL (measured, 3.11.5-057). A
  comment with `--` works in InfluxQL (measured, 1.13.1-032).

## Principals

- InfluxDB 3 Core has admin tokens and no users, so it has no ordinary user
  (the token guides and the dbmeta entry). Step 6 recorded only the
  administrator on InfluxDB 3.
- On InfluxDB 1, the ordinary user `dbmeta_user` has `READ` on `dbmeta`
  (the dbmeta entry). It can `SELECT`, `SHOW DATABASES`, which lists only
  `dbmeta`, and `SHOW QUERIES` (measured, 1.13.1-062 and 076). It cannot
  write, `DROP MEASUREMENT`, `KILL QUERY` or `SHOW DIAGNOSTICS`, and each is
  HTTP 403 (measured, 1.13.1-077 to 082).
- On InfluxDB 2, the ordinary user is a v1 user with read on the bucket
  `dbmeta` (the dbmeta entry). It can `SELECT`. It cannot write, which is
  HTTP 403, or `DROP MEASUREMENT`, which is HTTP 200 with
  `insufficient permissions` (measured, 2.9.1-080 and 081).
- The version is the header `X-Influxdb-Version` of `GET /ping`: `1.13.1`,
  `v2.9.1`, `3.9.13` or `3.11.5` (measured, 1.13.1-005, 2.9.1-005 and 3.11.5-005).
  InfluxDB 2 writes a `v` before the number. On InfluxDB 1 and 2 an ordinary
  user reads it, because `/ping` needs no credentials (measured, 1.13.1-061
  and 2.9.1-061). On InfluxDB 3, `/ping` also answers a JSON body with
  `product_name`, `version` and `revision` (measured, 3.11.5-005).
- `SELECT version()` gives the version of DataFusion, not of InfluxDB
  (measured, 3.11.5-053). `SHOW DIAGNOSTICS` gives the version on InfluxDB
  1 to the administrator only, and InfluxDB 2 and 3 do not have it
  (measured, 1.13.1-027, 2.9.1-027 and 3.11.5-054).

## Flavors

The driver serves InfluxDB 1, InfluxDB 2, and InfluxDB 3 and later (D78).

| Flavor | `X-Influxdb-Build` | Port | SQL | InfluxQL |
| --- | --- | --- | --- | --- |
| InfluxDB 1 | `OSS` | 8086 | no | yes, and chunks stream |
| InfluxDB 2 | `OSS` | 8086 | no, `/api/v3/query_sql` gives HTML | yes, through the v1 API |
| InfluxDB 3 Core | `Core` | 8181 | yes | yes |

- The rows of this table are measured on every release, except the ports,
  which come from the dbmeta entry and the image documents.
- With `sqlmode` set to `prefer` or `require`, the driver tells the flavors
  apart by the major number of `X-Influxdb-Version`, after it removes a
  leading `v`. With `disable` or `allow`, the key `version` names it (D78).
- InfluxDB 3 Enterprise, InfluxDB Cloud Serverless, Cloud Dedicated and
  Clustered speak the same endpoints, and differ in their tokens and limits
  (Gemini and DeepSeek, not measured). None of them meets R now.
- InfluxDB 1 Enterprise sends `ENT` in `X-Influxdb-Build` (the v1 API
  reference, not measured).

## Interfaces

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. Connect sends GET /ping for sqlmode prefer and require (D78). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1 or SHOW MEASUREMENTS, which checks the credentials and the database, as GET /ping does not on InfluxDB 1 and 2. |
| `driver.SessionResetter` | no | A connection holds no state on the server, so nothing needs a reset. |
| `driver.Validator` | no | A connection holds no state on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps a uint64 and a decimal, which the default converter refuses or turns into text. |
| `driver.QueryerContext` | yes | The server binds each argument itself, from params, except in INSERT, which the driver binds (D85). |
| `driver.ExecerContext` | yes | Exec reads every result to its end. InfluxDB counts no rows. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | BeginTx returns dbimp.ErrNotSupported, because InfluxDB has no transactions (D20). |
| `driver.RowsColumnScanner` | yes | A value is decoded when it is scanned, by its type in SQL (D80) and by its text in InfluxQL (D83). |
| `driver.RowsNextResultSet` | yes | Each series of InfluxQL is a result set (D81). SQL has one statement and one result. |
| `driver.RowsColumnTypeScanType` | yes | SQL gives the type from DESCRIBE (D80). InfluxQL names no types, so each is any. |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | SQL gives the data_type of DESCRIBE in upper case. InfluxQL names no types. |
| `driver.RowsColumnTypeLength` | no | No column of InfluxDB has a length. |
| `driver.RowsColumnTypeNullable` | yes | SQL gives is_nullable of DESCRIBE. InfluxQL names no types. |
| `driver.RowsColumnTypePrecisionScale` | yes | SQL gives the precision and the scale of a decimal from DESCRIBE. |
<!-- /dbimp:interfaces -->

## Faults

`usql` has no driver for InfluxDB, so there is no driver in use whose faults
this driver must not repeat. These facts about the other clients are for step
5a:

- The Go client queries through Flight SQL, and needs
  `github.com/apache/arrow-go/v18`, `google.golang.org/grpc` and
  `google.golang.org/protobuf` (its `go.mod`). D13 allows none of them
  without the approval of Ken.
- `github.com/influxdata/influxdb1-client` was last pushed on 2024-03-12
  (GitHub). Its code was not read.

## Second opinions

Step 3 took these leads from Gemini and DeepSeek, and step 6 tested them:

- Both said that a NULL in `json` leaves out its key. True (measured).
- Gemini said that a time arrives with no zone, and DeepSeek said that it
  arrives with `Z`. Both are true: `Timestamp(ns)` has no zone and
  `Timestamp(ns, "UTC")` has `Z` (measured).
- Gemini said that `format=parquet` keeps the schema of a result with no
  rows. Not measured, because the driver does not read Parquet (D13).
- DeepSeek said that an `Accept` of `application/vnd.apache.arrow.stream`
  gives an Arrow stream. False: it is HTTP 400 (measured).
- Both said that SQL has no `INSERT`, `UPDATE`, `DELETE` or transactions,
  and that `/api/v3/query_sql` takes one statement. True (measured).
- Both said that the server stops a query when the client disconnects. True
  on InfluxDB 3 (measured). Not measured on 1 and 2.
- Gemini said that InfluxDB 2 has no `SHOW DIAGNOSTICS`, `SHOW QUERIES` or
  `KILL QUERY`. True (measured).

Step 7 asked Gemini and DeepSeek again on 2026-09-28, and tested each lead
on 1.13.1, 2.9.1 and 3.11.5:

- Gemini said that a SQL parameter works only in `WHERE`. False: it works
  in `SELECT` and `LIMIT` too (measured, 3.11.5-031 and 117).
- Both said that a SQL parameter cannot be an array. True: an array and an
  object are HTTP 400 (measured, 3.11.5-114 and 115).
- DeepSeek said that the key of a positional parameter can be `"1"` or
  `"$1"`. Only `"1"` works (measured, 3.11.5-033 and 116).
- Gemini said that an InfluxQL parameter cannot bind a time string. False:
  it works on every release (measured, 3.11.5-119). DeepSeek said to test a
  time as an integer. It works on 1 and 2, and fails on 3 (measured).
- Both said that an InfluxQL parameter takes only scalar values. True: an
  array fails on every release (measured).
- Gemini said that `max-row-limit` caps a result that is not chunked on 1
  and 2. The default does not cap: 20,000 rows arrived (measured). A limit
  that is set was not measured.
- Gemini said that InfluxDB 3 Core limits a query to 72 hours of data. A
  query over three years of data succeeded (measured, 3.11.5-021). The
  configuration reference says that the limit is on the number of Parquet
  files, which this data does not reach.
- DeepSeek said that InfluxDB 3 can send a header with the id of a query.
  False: `/api/v3/query_sql` sends none (measured, 3.11.5-118).
- Gemini said that InfluxDB 2 and 3 need `Authorization: Bearer`, unlike
  InfluxDB 1. False: HTTP Basic works on every release (measured).
- Both named memory limits on InfluxDB 3, such as `--query-memory-bytes`,
  and the flavors of InfluxDB 3 in the cloud. Not measured.

## Open questions

Ken settled each question of steps 6 and 9 on 2026-09-28 and 2026-09-29:
D80 to D83, D85, D86 and D96. Ken decided the questions of the review of D97 on 2026-09-29, in D109,
D111 and D115. These facts remain:

- `features.json` holds one verdict for each entry, and several entries hold
  on some releases only. This document names the releases of each.
- From dbmeta `415e830`, the `url` that `dbrun dsn` prints for each
  InfluxDB principal is the DSN of D82, such as
  `influxdb://_admin:<token>@127.0.0.1:<port>/dbmeta`. The Test step of the
  workflow passed with it on 1.13.1, 2.9.1 and 3.11.5 on 2026-09-29.

## The test runs

The integration tests passed on 2026-09-28, run one release at a time from
`dbrun`, as the administrator and, on InfluxDB 1 and 2, as the ordinary
user: 1.11.8, 1.13.1, 2.8.0, 2.9.1, 3.9.13, 3.10.6 and 3.11.5. They include
the round trip of every type in each dialect (step 14a). The version, from
`GET /ping`, reads the same for both principals on InfluxDB 1 and 2, because
`/ping` needs no credentials there (measured, 1.13.1-061 and 2.9.1-061).
InfluxDB 3 Core has no ordinary user.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-29 from the staged code, which holds D96. A fact of
Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of InfluxDB
from the sections above.

### The server

| | Couchbase | InfluxDB |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | SQL: `POST /api/v3/query_sql`, on InfluxDB 3 only. InfluxQL: `POST /query`, with the form value `q` |
| Database | The key `query_context` of the body | `db` in the request, from the path of the DSN (D82) |
| Language | SQL++, which is close to SQL | SQL on InfluxDB 3, and InfluxQL on every release (D78) |
| DDL | In SQL++ | Few statements. `DROP MEASUREMENT` and `DROP SERIES` work on InfluxDB 1, and InfluxDB 3 refuses each |
| Writes | DML in SQL++ | Line protocol to `/write`. No server parses `INSERT` |
| Parameters | `?`, `$1` and `$name` | `$name` and `$1`, from `params` |
| Framing | One body for the whole result, which does not page | SQL: one array of objects. InfluxQL: one object of results, or a chunk for each 10,000 rows on InfluxDB 1 (D83) |
| Columns | `signature`, before the first row | SQL: no list, and a NULL leaves out its key. InfluxQL: `columns`, before the rows of each series |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | SQL: the statement. InfluxQL: the columns of each series |
| Errors | Can come with HTTP 200, after some rows | SQL: an error after some rows cuts the body with HTTP 200, and no text. InfluxQL: an error of a later statement comes with HTTP 200, after the earlier rows |
| Types | JSON. No date, decimal, UUID or binary | SQL: the types of `DESCRIBE`, with a decimal, a time and binary. InfluxQL: JSON, and the time as text |
| Cancel | The server stops a query when the client leaves | InfluxDB 3 stops a query when the client leaves. For InfluxDB 1 and 2 it is not measured |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None |
| Several statements | Refused | SQL: refused. InfluxQL: taken, with a result for each |
| Authentication | Basic, or `creds` in the body | Basic. A token is the password (D82) |
| Default port | 8093, or 18093 with TLS | 8086 for InfluxDB 1 and 2, and 8181 for InfluxDB 3, with or without TLS |

The differences that a caller sees:

- The dialect depends on the release, and the key `sqlmode` chooses it.
  Its default asks `GET /ping` for the release when the driver connects
  (D78).
- SQL learns its columns and their types from `DESCRIBE`, because the answer
  leaves out every NULL (D80).
- Each series of InfluxQL is a result set, whose first columns are
  `measurement` and the tags (D81 and D96).
- `INSERT` of line protocol is the work of the driver, which sends it to
  `/write` (D85).
- `BeginTx` fails, because InfluxDB has no transactions (D20).

### The driver

| | `couchbase` | `influxdb` |
| --- | --- | --- |
| Size, without tests, on 2026-09-29 | About 1300 lines in 8 files | About 2800 lines in 11 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Database`, `RetentionPolicy`, `SQLMode`, `Version`, `Describe`, `Chunked` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithRetentionPolicy`, `WithChunked` and `WithDescribe`, through `WithOptions` or an argument (D109). `WithParameter` sets any key of the JSON body of SQL, or of the form of InfluxQL. `WithTimeout` and `WithReadonly` give `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name`. Any value that JSON encodes | Sent in `params`, by name or by ordinal. A `[]byte`, a NaN, an infinity, a map and a slice are refused. `INSERT` writes each argument as a literal of line protocol (D85) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | SQL: `dbimp.ObjectRows`, with the columns of `DESCRIBE`. InfluxQL: `dbimp.ArrayRows` for each series |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | SQL: the same three from `DESCRIBE`, and `ColumnTypePrecisionScale`. InfluxQL: none |
| Result sets | One | SQL: one. InfluxQL: one for each series, through `RowsNextResultSet` |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44). A time is a string | SQL: by the type of `DESCRIBE`, with `uint64`, `*apd.Decimal`, `time.Time` and `[]byte`. A NaN or an infinity reads as `math.NaN()` (D80). InfluxQL: `int64`, `uint64` or `float64` by the text of the number, and `time` as a `time.Time` (D83) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK` | `BeginTx` returns `dbimp.ErrNotSupported` |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same (D36), with no decision of its own for InfluxDB |
| Errors | `*ResponseError`, with a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Statement, Message}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx |
| Authentication | Basic | `auth=basic`, or `auth=bearer`, which sends `Token` to InfluxDB 2 and `Bearer` to the other releases (D116) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Version` and `Dialect` for `Conn.Raw`, `SQL` and `InfluxQL`, and the constants of `sqlmode`, `describe` and `chunked` |

The differences that a caller sees:

- A connection can send `GET /ping` when it opens, for `sqlmode=prefer` and
  `sqlmode=require` (D78).
- `RowsAffected` returns an error. That is a question below.
- A NaN, an infinity and a minus infinity of SQL all read as a NaN, because
  the JSON of the server writes all three as `null` (D80).
- A whole float of InfluxQL reads as an `int64`, because the answer holds no
  types (D83).
- Ken decided these on 2026-09-29:
  - The driver closes the body, as Couchbase does, because every release
    stops a query when the client leaves (D115).
  - The driver takes the four options that D109 gives every driver, and an
    option for each of `rp`, `chunked` and `describe`. W15 of
    [BACKLOG.md](BACKLOG.md) added them. `WithTimeout` and `WithReadonly`
    give `dbimp.ErrNotSupported`, because no release has either for one
    request.
  - The port does not change with `tls=true`, `RowsAffected` returns an
    error, and InfluxQL binds `$1` (D111).
