# Trino and Presto

This file holds what is known about Trino and Presto, for the driver `trino`
(W31 and D173). The headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a to 7 measured `trino-476`, `trino-483` and `presto-0.299` on
2026-10-07, as `trino` and `presto`, the administrators that `dbrun` names.
None of the three has an ordinary user, because no release asks for a password
(see Principals). A fact marked "recorded" is in `testdata/trino/`, and the
name in quotes after it is the name of its request in `requests.json` there.
The three releases gave the same answers, except where a line names a flavor
or a release. A fact marked "measured" names how it was measured. A fact
marked "not measured" names its source. The sources, each read on
2026-10-07, are these:

- "The dbmeta entries" are `container/trino.go` and `container/presto.go` in
  `dbmeta`. "dbmeta D73" is its decision that Presto is a dialect of its own.
- "dburl" is `scheme.go` and `dsn.go` in `dburl`.
- "usql" is `drivers/trino/trino.go` and `drivers/presto/presto.go` in
  `usql`, and its `go.mod`.
- "trino-go-client" is `github.com/trinodb/trino-go-client` at commit
  `bcbbced` of 2026-10-04, with its issues.
- "presto-go-client" is `github.com/prestodb/presto-go-client` at commit
  `500bbe9` of 2026-05-18, with its issues.
- "trino-python-client" is `github.com/trinodb/trino-python-client` at commit
  `74c6156` of 2026-09-23, the official Python client of the Trino project.
- "Gemini" is `gemini-3.1-pro-preview`, and "DeepSeek" is `deepseek-v4-pro`,
  asked on 2026-10-07.
- "The recorder" is `dbimptest/cmd/record`. It cannot send a request to the
  absolute `nextUri` of these servers, and it cannot keep a response header.
  A copy of its `main.go` in the scratch folder made the recordings, with two
  changes. It reads the host of a `nextUri` and sends the request to the
  host of the principal, and it keeps a header of a response with the
  capture `header:<name>`. The shared code needs both changes (see Open
  questions).

## Summary

- Trino and Presto are engines that run SQL over data that other systems
  hold. A catalog is a connector with a name, and a schema is a group of
  tables in a catalog. A table has the name `catalog.schema.table`. Both
  engines come from one program that split in 2019 (dbmeta D73, not
  measured). The two differ in many answers (see Flavors).
- R holds. `dbrun` starts `trino-476`, `trino-483` and `presto-0.299` (the
  dbmeta entries). The images are `docker.io/trinodb/trino` and
  `docker.io/prestodb/presto`, and each one listens on port 8080 in the
  container. `GET /v1/info` gave the version of each: `476`, `483` and
  `0.299-7d50721` (recorded: "the info of the server").
- H and S hold. `POST /v1/statement` took SQL and answered with HTTP 200 on
  each release (recorded: "a statement").
- The priority is in [TARGETS.md](TARGETS.md): Trino and Presto are number 16
  of the order (D173). D173 decides that one driver serves both.
- `dburl` has the scheme `trino`, with the generator `GenTrino`, the aliases
  `trino`, `trinos` and `trs`, and the dialect `trino`. It has the scheme
  `presto`, with the generator `GenPresto`, the alias `prestodb` and the
  dialect `presto` (dburl, `scheme.go`).
- `usql` has a driver for each scheme. The scheme `trino` uses
  `github.com/trinodb/trino-go-client/trino` v0.336.0, and the scheme
  `presto` uses `github.com/prestodb/presto-go-client/v2` v2.1.2 (usql,
  `go.mod`). Neither file of `usql` sets a `Version` statement (usql).
- The only catalogs that write are `memory` on each release. The other
  catalogs are `system`, `jmx`, `tpch` and `tpcds`, and they read only
  (recorded: "schema: show catalogs"). `memory` refuses `UPDATE`, `DELETE`
  and `MERGE` on each release (recorded: "crud: update", "crud: delete" and
  "crud: merge"). `memory` refuses `TRUNCATE` on Presto only (recorded:
  "crud: truncate"). A catalog that accepts `UPDATE` and `DELETE`, such as
  Iceberg, is not in the `dbrun` images, and `CREATE CATALOG` fails with
  `CREATE CATALOG is not supported by the static catalog store` (measured
  with `curl` on trino-476 on 2026-10-07). DRIVER.md says to stop and ask Ken
  when a server refuses one of insert, select, update and delete. See Open
  questions.
- The driver is `github.com/xo/dbimp/trino`, and D175 holds its decisions of
  step 9. The integration tests ran on `trino-476`, `trino-483` and
  `presto-0.299` on 2026-10-07 (see Integration tests).

## Requests

These facts were recorded on each release:

- A statement is `POST /v1/statement` with the SQL text as the body. The
  server takes the body as text, whatever the `Content-Type` is. A body sent
  as `application/json` is read as SQL, and `{"query": ...}` fails with a
  syntax error (recorded: "a statement", "a statement with no content type"
  and "a statement as JSON"). `GET /v1/statement` answers HTTP 405 (recorded:
  "a GET of the statement endpoint").
- The user is the header `X-Trino-User` on Trino, and `X-Presto-User` on
  Presto. The catalog and the schema are the headers `X-Trino-Catalog` and
  `X-Trino-Schema`, or `X-Presto-Catalog` and `X-Presto-Schema`. A statement
  with no catalog and no schema runs when it names no table, and it names the
  missing schema in an error when it names a table by its short name. A table
  with its full name works with no header (recorded: "a statement with no
  catalog and no schema", "a table with no catalog and no schema" and "a
  table with its full name and no catalog header").
- Presto ignores the headers of Trino: a request with `X-Trino-User` fails
  with `User must be set` (measured with `curl` on presto-0.299 on
  2026-10-07). Whether Trino ignores the headers of Presto is not measured.
- The answer to the `POST` has no rows. It holds `id`, `infoUri`, `nextUri`,
  `stats` and `warnings`. The client then sends `GET` to `nextUri` until an
  answer has no `nextUri` (recorded: "a statement"). Each `nextUri` is an
  absolute URL, with the host of the `Host` header of the request.
- Trino puts the token in the path: `/v1/statement/queued/<id>/<token>/<n>`
  and `/v1/statement/executing/<id>/<token>/<n>`. Presto puts the number in
  the path and the token in the query: `/v1/statement/queued/<id>/<n>?slug=<token>`
  (recorded: "a statement").
- A poll needs no user header (recorded: "a poll with no user header").
- A query id has the form `20261006_232423_00058_x3v5n`, which is a date, a
  time, a number and a code of the server. `X-Trino-Source`,
  `X-Trino-Client-Info`, `X-Trino-Client-Tags`, `X-Trino-Trace-Token` and
  `X-Trino-Language` are accepted (recorded: "a statement with the source and
  the client info"). The server accepts a header that it does not know and a
  value of 4000 bytes (recorded: "a statement with a header that is not
  known" and "a statement with a long header value").
- The server sends gzip when the request has `Accept-Encoding: gzip`, with
  `Content-Encoding: gzip`, on the statement pages and on `/v1/info`
  (recorded: "a statement with gzip" and "the info with gzip"). `zstd` in
  `Accept-Encoding` gives plain JSON on trino-476 and on presto-0.299. On
  trino-483 the answer has `Content-Encoding: zstd`, and the recorder holds
  its body as binary and cannot follow its `nextUri` (recorded: "a statement
  with zstd").
- `GET /` and `GET /ui` answer HTTP 303 with `Location: /ui/` on Trino and
  HTTP 307 on Presto (recorded: "a redirect from the root" and "a redirect
  from the ui").

These facts come from the sources, and are not measured:

- trino-go-client sends `POST /v1/statement` with the headers
  `X-Trino-User`, `X-Trino-Source`, `X-Trino-Catalog`, `X-Trino-Schema`,
  `X-Trino-Session`, `X-Trino-Client-Capabilities` and
  `X-Trino-Query-Data-Encoding` (trino-go-client, `trino/trino.go`).
- trino-python-client sends the same headers, and retries a request on HTTP
  429, 502, 503 and 504, and on an answer with HTTP 200 and an empty body
  (trino-python-client, `client.py`).
- The Java client sends `HEAD` to a heartbeat URI while it reads a spooled
  result (trino-go-client, `trino/trino.go`, `startHeartbeat`).

## The DSN

These facts come from the sources, and are not measured:

- `dburl` writes `http://user@localhost:8080?catalog=<catalog>&schema=<schema>`
  for the scheme `trino`. It writes `https` and the port 8443 when the scheme
  ends in `s`. The catalog is the first part of the path, and the schema is
  the second (dburl, `GenTrino`).
- `dburl` writes `presto://user@localhost:8080/<catalog>/<schema>` for the
  scheme `presto`. The query holds the options of the client. A TLS option
  moves the port to 8443, and `presto` has no alias that ends in `s` (dburl,
  `GenPresto`).
- `dbrun` gives `http://trino@127.0.0.1:<port>?catalog=memory&schema=default`
  as the `dsn` and the `api` of a Trino release, and
  `trino://trino@127.0.0.1:<port>/memory/default` as its `url`. It gives
  `presto://presto@127.0.0.1:<port>/memory/default` as the `dsn` and the
  `url` of Presto, and it gives no `api` for Presto. The recorder took
  `http://127.0.0.1:<port>`, built from the host and the port of that `url`
  (dbmeta D167, and recorded: every file).
- trino-go-client takes `http` and `https`, the catalog and the schema in the
  query or in the path, and the keys `source`, `session_properties`,
  `extra_credentials`, `roles`, `clientTags`, `trace_token`, `client_info`,
  `language`, `timezone`, `custom_client`, `query_timeout`,
  `request_retry_timeout`, `request_retry_max_attempts`, `heartbeat_interval`,
  `accessToken`, `explicitPrepare`, `forwardAuthorizationHeader`,
  `resourceEstimates`, `externalAuthentication` and the keys for TLS and
  Kerberos. It refuses a password on a URL that is not `https`
  (trino-go-client, `ParseDSN` and `requireTLSForPassword`).
- presto-go-client takes `presto` and `trino` as the scheme, and the catalog
  and the schema in the path. It reads any other key as a session property
  (presto-go-client, `driver.go`, and dbmeta, `container/presto.go`).
- Neither server has a database to choose. The catalog and the schema are the
  names of the session, and a statement can set them: `USE memory.default`
  answers `X-Trino-Set-Catalog` and `X-Trino-Set-Schema`, and the client sends
  them in the next request (recorded: "use").

## Responses

These facts were recorded on each release:

- The answer is JSON. A page holds `id`, `infoUri`, `nextUri`, `stats` and
  `warnings`. A page can also hold `columns`, `data`, `updateType`,
  `updateCount`, `partialCancelUri` and `error` (recorded: "a statement").
  The `nextUri` is absent on the last page.
- The columns come in `columns` as a list of `name`, `type` and
  `typeSignature`. They arrive before the first row. On Trino, the first page
  with `columns` has no `data`, and the next page holds the first rows. On
  Presto, a page with `columns` can hold rows too, and the first rows can
  arrive on the second poll (recorded: "columns of a statement with rows").
- A page with rows has `data`, a list of rows. A row is a list of values in
  the order of the columns. A page with no rows has no `data` member. A
  statement that returns no rows still sends `columns` (recorded: "columns of
  a statement with no rows").
- The names of the columns are the names of the statement. A name that the
  statement does not give is `_col0`, `_col1` and so on. Two columns can have
  the same name (recorded: "columns with a repeated name and an unnamed
  one"). The order of the columns is the order of the statement (recorded:
  "columns in a declared order that is not the table order").
- `stats.state` is `QUEUED`, `STARTING`, `RUNNING`, `FINISHING`, `FINISHED`
  or `FAILED` on Trino. Presto starts with `WAITING_FOR_PREREQUISITES`
  (recorded: "a statement"). The state is `FINISHED` on the last page, which
  the server sends after the client read the pages before it.
- A statement that writes sends `updateType`, such as `INSERT`, and
  `updateCount` when it knows the count. A write with a count sends the
  column `rows`, and one row with the count (recorded: "columns of a
  statement that writes"). A statement of DDL, such as `CREATE TABLE`, sends
  no column and no row on Trino. On Presto it sends the column `result`, and
  one row with `true` (recorded: "schema: create table").
- A `nextUri` that the client read once can be read again until the client
  reads the next one. After that the old one answers HTTP 410 (recorded:
  "reread: poll four after poll five", on Trino, and "reread: poll two after
  poll three", on Presto). The answer to a repeated read is the same page
  (recorded: "reread: poll four again").
- A query that does not exist, or a token that is wrong, answers HTTP 404
  (recorded: "a poll of a query that does not exist" and "a poll with the id
  and a wrong token").
- The size of a page is not set by the client. Trino sent pages of 255 rows,
  then 2045 rows, then none, for 2300 rows of 200 bytes. For 2300 rows of
  1000 bytes it sent 255, 1792 and 253 rows, and the middle page was 1.8
  megabytes (recorded: "2300 rows of 200 bytes" and "2300 rows of 1000
  bytes"). No cap on the number of rows appeared: all 20000 rows of a
  statement arrived, in pages that grew from 255 rows to thousands (recorded:
  "20000 small rows").
- The query parameter `targetResultSize=100kB` on the poll made Presto send
  smaller pages of 768 and 1024 rows. Trino sent the same pages as with no
  parameter (recorded: "page size: poll 3", on Presto, and "page size: poll
  5", on Trino). The headers `X-Trino-Max-Size`, `X-Presto-Max-Size` and
  `X-Trino-Target-Result-Size` changed nothing on either flavor (recorded:
  "page size by header: poll 5" and "page size by header: poll 2").
- `X-Trino-Query-Data-Encoding: json+zstd` gives plain JSON, because the
  images do not turn spooling on (recorded: "a statement with a query data
  encoding that the server lacks"). The spooling protocol is not measured.
- Compression is in Requests. A redirect is in Requests. The server does not
  redirect a statement.
- A response header can carry the state of the session: `X-Trino-Set-Session`,
  `X-Trino-Clear-Session`, `X-Trino-Set-Catalog`, `X-Trino-Set-Schema`,
  `X-Trino-Set-Role`, `X-Trino-Added-Prepare`, `X-Trino-Deallocated-Prepare`,
  `X-Trino-Started-Transaction-Id` and `X-Trino-Clear-Transaction-Id` (recorded:
  "a statement that sets a session property", "use", "prepare", "deallocate",
  "start a transaction with the header NONE" and "rollback"). Presto sends
  the same names with `X-Presto-`. The headers arrive on the page of a poll,
  and not on the answer to the `POST`.
- `X-Trino-Set-Path` arrives for `SET PATH` on Trino, when the request sends
  the capability `PATH` (recorded: "set path with the capability").
- Presto adds `Cache-Control` to the pages. The answer to the `POST` has
  `max-age=60`, and the pages have `max-age=300` and less (recorded: "a
  statement").
- On Trino, `partialCancelUri` holds the host of the request. On Presto it
  holds the internal address of the server, such as `192.168.1.5:8080`, which
  the client cannot reach (recorded: "a statement").

These facts come from the sources, and are not measured:

- The Python client acknowledges the data of a page by reading the next
  `nextUri`, and a query ends as `FINISHED` only after the client read every
  page (trino-python-client, `client.py`).
- trino-go-client decodes each page into memory, and it holds a spooled
  segment in memory until the caller reads it (trino-go-client,
  `trino/trino.go`).

## Types

Each type that the survey marks `yes` has a row in the table of step 8a.
The wire type is the name of the type in `columns[].type`, in upper case,
without its arguments. A name with a flavor is a type that one flavor has.
The `type` of a column is lower case on Trino and Presto, except for
`HyperLogLog`, `P4HyperLogLog`, `SetDigest`, `Geometry` and `SphericalGeography`
(recorded: "types: boolean, integers, floats, decimals, text and bytes"). Trino
writes the interval types in upper case, and Presto writes them in lower case
(recorded: "types: intervals, json, uuid and ipaddress"). Trino writes
`decimal(38, 0)` with a space after the comma, and Presto writes
`decimal(38,0)` (recorded: the same request).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INTEGER | integer | `int64` | `int64` | `INTEGER` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| REAL | float | `float64, with the infinities and NaN from their strings` | `float64` | `REAL` | yes |
| DOUBLE | float | `float64, with the infinities and NaN from their strings` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `*apd.Decimal, from the JSON string` | `*apd.Decimal` | `DECIMAL` | yes |
| CHAR | string | `string, with the padding of spaces that the server sends` | `string` | `CHAR` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| VARBINARY | binary | `[]byte, from the base64 in a string` | `[]uint8` | `VARBINARY` | yes |
| JSON | json | `the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any, from the JSON text in a string` | `interface {}` | `JSON` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)` | `dbimp.LocalTime` | `TIME` | yes |
| TIME WITH TIME ZONE | time of day with offset | `dbimp.OffsetTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)` | `dbimp.OffsetTime` | `TIME WITH TIME ZONE` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| TIMESTAMP WITH TIME ZONE | timestamp | `time.Time, in the zone that the text names, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)` | `time.Time` | `TIMESTAMP WITH TIME ZONE` | yes |
| INTERVAL YEAR TO MONTH | interval | `dbimp.Interval, with Months` | `dbimp.Interval` | `INTERVAL YEAR TO MONTH` | yes |
| INTERVAL DAY TO SECOND | interval | `dbimp.Interval, with Days and Nanoseconds` | `dbimp.Interval` | `INTERVAL DAY TO SECOND` | yes |
| ARRAY | array | `[]any, of the Go types of its elements, from the JSON array. On Presto, from the JSON text in a string` | `[]interface {}` | `ARRAY` | yes |
| MAP | map | `map[string]any, with the keys as the strings of the JSON object. On Presto, from the JSON text in a string` | `map[string]interface {}` | `MAP` | yes |
| ROW | tuple | `[]any, in the order of the fields, from the JSON array. On Presto, from the JSON text in a string` | `[]interface {}` | `ROW` | yes |
| UUID | uuid | `uuid.UUID` | `uuid.UUID` | `UUID` | yes |
| IPADDRESS | other | `string, with the text of the server` | `string` | `IPADDRESS` | yes |
| HYPERLOGLOG | binary | `[]byte, from the base64 in a string` | `[]uint8` | `HYPERLOGLOG` | yes |
| P4HYPERLOGLOG | binary | `[]byte, from the base64 in a string` | `[]uint8` | `P4HYPERLOGLOG` | yes |
| SETDIGEST | binary | `[]byte, from the base64 in a string` | `[]uint8` | `SETDIGEST` | yes |
| QDIGEST | binary | `[]byte, from the base64 in a string` | `[]uint8` | `QDIGEST` | yes |
| TDIGEST | binary | `[]byte, from the base64 in a string` | `[]uint8` | `TDIGEST` | yes |
| GEOMETRY | geometry | `string, the WKT text of the server` | `string` | `GEOMETRY` | yes |
| SPHERICALGEOGRAPHY | geometry | `string, the WKT text of the server` | `string` | `SPHERICALGEOGRAPHY` | yes |
| UNKNOWN | null | `nil` | `interface {}` | `UNKNOWN` | yes |
| trino 483 NUMBER | decimal | `*apd.Decimal, from the JSON string, with NaN and the infinities of apd` | `*apd.Decimal` | `NUMBER` | yes |
| trino 483 VARIANT | json | `the decoded JSON value, from the JSON value of the capability VARIANT` | `interface {}` | `VARIANT` | yes |
<!-- /dbimp:types -->

The mapping is a proposal of step 8a, and Ken has not reviewed it. A type
whose Go type differs from the Go type of its kind, or that fits no kind, has
a reason:

- IPADDRESS fits no kind. TYPES.md has no kind for an address, so the type is
  `other`, and its Go type is the text of the server.
- The sketches, and the digests, are opaque bytes, so they are `binary`, as
  the `HLLSketch` of Druid is. Their Go type is the Go type of their kind.
- TIME, TIME WITH TIME ZONE, TIMESTAMP and TIMESTAMP WITH TIME ZONE have up to
  12 digits of fraction, which are picoseconds. The Go types have
  nanoseconds. DeepSeek said that a value cut short breaks the rule that a
  value that cannot be decoded is an error. See Open questions.
- A Trino 483 NUMBER is a number of any size, and it can be `NaN`. The kind
  `decimal` fits it only because `apd` holds NaN, the infinities and an
  exponent of 1000. DeepSeek said that NUMBER is not a DECIMAL. See Open
  questions.

These facts were recorded on each release, from literals, and from the table
`dbimp_types` (recorded: "setup: make the table of every type that the memory
catalog stores", and the requests whose names start with `types:`):

- A NULL is the JSON `null` in every type. A column whose type is `unknown`
  is always `null`. The type of a bare `NULL` is `unknown` (recorded: "types:
  null of each type" and "types: empty values").
- An integer is a JSON number and keeps every digit from -9223372036854775808
  to 9223372036854775807. `BIGINT '9223372036854775807' + 1` fails with
  `bigint addition overflow` (recorded: "types: boolean, integers, floats,
  decimals, text and bytes" and "an error before any row: overflow").
- A `REAL` and a `DOUBLE` are JSON numbers, such as `1.7976931348623157e+308`
  and `3.4028235e+38`. An infinity and a NaN are the strings `"Infinity"`,
  `"-Infinity"` and `"NaN"`. A negative zero is `-0.0` (recorded: "types: nan,
  infinity and negative zero").
- A `DECIMAL` is a JSON string with every digit, up to 38 digits, such as
  `"1234567890123456789012345678901234567.8"`. The column type names the
  precision and the scale, such as `decimal(38, 1)` (recorded: "types:
  boolean, integers, floats, decimals, text and bytes").
- A `VARCHAR` is a JSON string, and an empty string stays empty. A `CHAR(n)`
  has its padding of spaces. `CAST('' AS char(3))` is three spaces (recorded:
  "types: empty values"). A string literal has the type `varchar(n)` (recorded:
  "columns with a repeated name and an unnamed one").
- A `VARBINARY` is a base64 string, and an empty value is `""` (recorded:
  "types: empty values").
- A `JSON` is a string that holds JSON text. `JSON 'null'` is the string
  `null`, and `JSON '"x"'` is the string `"x"` with its quotes (recorded:
  "types: intervals, json, uuid and ipaddress"). A SQL NULL is the JSON
  `null`, with no string.
- A `DATE` is `2026-10-01`, `-0001-01-01` and `9999-12-31` (recorded: "types:
  date, time and timestamp with the default capabilities" and "types: date
  before the year 1").
- A `TIME` and a `TIMESTAMP` have the text `12:34:56.789` and
  `2026-10-01 12:34:56.789`. Without the capability `PARAMETRIC_DATETIME`,
  Trino cuts the fraction to three digits and names the type `time` and
  `timestamp`. With the capability, it keeps up to 12 digits and names the
  type `time(12)` and `timestamp(12)`, such as `2026-10-01 12:34:56.123456789012`
  (recorded: "types: time and timestamp with 12 digits and the default
  capabilities" and "types: date, time and timestamp with the capability for
  parameters"). Presto has no such capability, and it refuses a time literal
  with more than three digits with `'12:34:56.123456789012' is not a valid
  time literal` (recorded: the same requests).
- A `TIME WITH TIME ZONE` is `12:34:56.789+05:30` on Trino and
  `12:34:56.789 +05:30` on Presto (recorded: "types: date, time and timestamp
  with the default capabilities").
- A `TIMESTAMP WITH TIME ZONE` has the text of the timestamp, a space, and the
  zone. The zone is a name, such as `Asia/Jakarta` or `UTC`, or an offset,
  such as `+05:30` (recorded: the same request). A cast gives the name
  `America/New_York` (recorded: "types: a cast to a time zone with a name").
- `INTERVAL YEAR TO MONTH` is `1-2`. `INTERVAL DAY TO SECOND` is
  `3 04:05:06.789` and `-1 00:00:00.000`, with three digits of fraction
  (recorded: "types: intervals, json, uuid and ipaddress").
- An `ARRAY` is a JSON array, and a `MAP` is a JSON object. The keys of a map
  are strings even for an integer or a date: `{"1": "x"}` and
  `{"2026-10-01": 1}` (recorded: "types: array, map and row"). A `ROW` is a
  JSON array in the order of the fields. The names of the fields are in the
  `typeSignature` of the column, and not in the value. An unnamed field has no
  name (recorded: "types: array, map and row"). An empty `ARRAY[]` has the type
  `array(unknown)`, and `MAP()` has `map(unknown, unknown)` (recorded:
  "types: empty values").
- On Presto, an `ARRAY`, a `MAP` and a `ROW` are strings of JSON text with
  spaces and new lines, such as `"[ 1, null, 3 ]"`. A container inside a
  container is a string again: `ARRAY[ARRAY[1], ARRAY[2, 3]]` is
  `"[ \"[ 1 ]\", \"[ 2, 3 ]\" ]"` (recorded: "types: array, map and row"). The
  setting is `nested-data-serialization-enabled` of the server, which the
  client cannot change (presto-go-client issue 103, not measured, and
  recorded: "types: array, map and row").
- A `UUID` and an `IPADDRESS` are strings, such as
  `12345678-1234-5678-1234-567812345678`, `10.0.0.1` and `::1` (recorded:
  "types: intervals, json, uuid and ipaddress").
- A `HyperLogLog`, a `P4HyperLogLog`, a `SetDigest`, a `qdigest(bigint)` and a
  `tdigest` are base64 strings. A `Geometry` and a `SphericalGeography` are
  WKT strings, such as `POINT (1 2)` (recorded: "types: sketch HyperLogLog",
  "types: sketch P4HyperLogLog", "types: sketch SetDigest", "types: sketch
  QDigest", "types: sketch TDigest" and "types: geometry"). Presto names the
  digest types `tdigest(double)` in a table, and Trino 476 and 483 name it
  `tdigest` (recorded: "types: the types that the tables store").
- Trino 483 has `NUMBER` and `VARIANT`, and sends them as such only when the
  request has the capability. Without it, a `NUMBER` is a `varchar` string
  and a `VARIANT` is a `json` string. With `NUMBER`, the value is a string,
  such as `"1"`, `"1E+1000"` and `"NaN"`. With `VARIANT`, the value is a JSON
  value. With `VARIANT_BINARY` too, it is an object `{"metadata": ..., "value": ...}`
  with two base64 strings (recorded: "types: number with no capability",
  "types: number with the capability", "types: variant with no capability",
  "types: variant with the capability" and "types: variant with the
  capability for binary"). Trino 476 and Presto refuse the two type names
  with `Unknown type` (recorded: the same requests).
- A server does not name a capability that it does not know. The request
  with `NOSUCH` ran normally (recorded: "types: a capability that the server
  does not know").
- The `memory` catalog stores each type of the table, as the column types
  show, and it refuses a column of type `unknown` (recorded: "types: the types
  that the tables store" and "types: a table with a column of unknown"). The
  column types of a stored table can differ in their text between releases:
  Trino 476 writes `row(a integer, b varchar)` and Trino 483 writes
  `row("a" integer, "b" varchar)` (recorded: "types: the types that the tables
  store").

These facts were measured with the integration tests of the driver on
2026-10-07:

- Presto writes the offset zero of a `TIME WITH TIME ZONE` as `UTC` with no
  space, such as `00:00:00.000UTC`, and the other offsets as `+05:30` after a
  space. The driver reads both.
- A query that fails after its columns arrive sends the error in a later page,
  so `QueryContext` reads up to the first row, or to the end of a result with
  none, and returns an error that comes before any row. An error after a row
  wraps `dbimp.ErrIncomplete` (D107).

These facts come from the sources, and are not measured:

- trino-go-client returns a `decimal`, a `char`, a `varchar`, an interval,
  an `ipaddress`, a `uuid`, a `Geometry` and a `SphericalGeography` as a
  `string`, a `varbinary` and each sketch as a `[]byte`, a date and a time
  type as a `time.Time`, and each integer as an `int64` (trino-go-client,
  `convertScalar`).
- trino-python-client maps `uuid` to `uuid.UUID` and `decimal` to
  `Decimal`, and a `ROW` to a named tuple (trino-python-client, `mapper.py`).

## Parameters

These facts were recorded on each release:

- The server binds no parameter that the client sends apart from the text. A
  parameter is a literal in the statement `EXECUTE <name> USING <literal>, ...`.
  The query that the statement runs is a prepared statement, which the client
  names in a request header.
- The header is `X-Trino-Prepared-Statement: <name>=<text>` on Trino and
  `X-Presto-Prepared-Statement` on Presto. The text is the SQL with `?` for
  each parameter, escaped as a URL. A request can hold the header more than
  once, for more than one statement (recorded: "a prepared statement named in
  the header" and "two prepared statements in the header"). A text that is not
  escaped fails on Presto, because it reads a plus sign as a space (recorded:
  "a prepared statement in the header with no encoding").
- A statement with `?` and no parameter fails with `Incorrect number of
  parameters: expected 1 but found 0` (recorded: "a question mark with no
  parameters"). Too many and too few parameters fail the same way (recorded:
  "parameters in the header: too many" and "parameters in the header: too
  few").
- A name that the header does not name fails with `Prepared statement not
  found` (recorded: "a prepared statement that the header does not name").
- Named parameters do not exist. `:a` is a syntax error on Trino, and an
  error of HTTP 400 on Presto (recorded: "parameters in the header: a named
  parameter").
- `PREPARE name FROM <statement>` answers `X-Trino-Added-Prepare:
  name=<escaped text>`, and `DEALLOCATE PREPARE name` answers
  `X-Trino-Deallocated-Prepare: name`. The client sends the prepared
  statements back in the header (recorded: "prepare" and "deallocate").
- A parameter can be a boolean, an integer, a double, a decimal, a date, a
  timestamp, bytes, an array, a row, a string with a quote and a backslash,
  and a NULL with a type (recorded: "parameters in the header: many types").
  A NULL needs a type, as in `CAST(NULL AS bigint)`. A bare `NULL` has the type
  `unknown` (recorded: "parameters in the header: three of them").
- `EXECUTE IMMEDIATE '<statement>' USING <literal>, ...` binds and runs in one
  request on Trino (recorded: "parameters with execute immediate"). Presto
  refuses it with a syntax error (recorded: the same request).
- A parameter works in `LIMIT` on Trino, and Presto refuses `LIMIT ?` with an
  error of HTTP 400 (recorded: "parameters in the header: a limit"). A
  parameter works in `WHERE`, and in `INSERT ... VALUES` (recorded:
  "parameters in the header: a where clause" and "parameters in the header: an
  insert").
- `DESCRIBE INPUT name` and `DESCRIBE OUTPUT name` answer (recorded: "describe
  input" and "describe output").

These facts were measured with the integration tests of the driver on
2026-10-07:

- Trino coerces an integer, a double or a decimal that a statement inserts into
  a column of another numeric type, such as an integer into a `tinyint` or a
  double into a `real`. Presto refuses each one with `Mismatch at column 2: 'v'
  is of type tinyint but expression is of type integer`, so a caller writes
  `CAST(? AS tinyint)` for Presto.
- Presto refuses a literal of a time or a time with a time zone that has more
  than three digits of fraction, so the driver writes three digits for Presto
  and fails for a value with a finer fraction. Trino takes up to twelve
  digits.
- A zero `dbimp.Interval` names no unit, so the driver writes it as an
  `INTERVAL DAY TO SECOND`. A column of `INTERVAL YEAR TO MONTH` refuses it
  with `TYPE_MISMATCH`, and a caller writes `INTERVAL '0-0' YEAR TO MONTH`.
- A bare `NULL` as a parameter takes the type of the column that it goes into,
  on both flavors.

These facts come from the sources, and are not measured:

- trino-go-client writes each parameter as a literal itself, with `Serial`,
  and sends `EXECUTE IMMEDIATE`, or the `PREPARE` header when `explicitPrepare`
  is on. Its named arguments are not parameters. They set headers, such as
  `X-Trino-User` (trino-go-client, `exec`).
- trino-python-client uses the paramstyle `qmark`. It tries `EXECUTE
  IMMEDIATE 'SELECT 1'` once, and uses `PREPARE` and `EXECUTE` when that fails
  (trino-python-client, `dbapi.py`).

## Transactions

These facts were recorded on each release:

- A transaction starts with `START TRANSACTION`, with the header
  `X-Trino-Transaction-Id: NONE`. The answer has `X-Trino-Started-Transaction-Id`
  with the id. Without the header of `NONE`, the statement fails with
  `INCOMPATIBLE_CLIENT: Client does not support transactions` (recorded: "start
  a transaction with the header NONE" and "start a transaction with no
  header"). Presto sends the same names with `X-Presto-`.
- The client sends the id in `X-Trino-Transaction-Id` in each later request.
  `COMMIT` and `ROLLBACK` answer `X-Trino-Clear-Transaction-Id: true`
  (recorded: "a select in a read only transaction", "commit" and "rollback").
- A transaction can set an isolation level and read only: `START TRANSACTION
  ISOLATION LEVEL READ COMMITTED, READ ONLY` ran (recorded: "start a read only
  transaction").
- A transaction in a transaction fails with `Nested transactions not
  supported` (recorded: "start a transaction in a transaction"). `COMMIT` with
  no transaction fails with `NOT_IN_TRANSACTION` (recorded: "commit with no
  transaction"). An id that the server does not know fails with
  `UNKNOWN_TRANSACTION` (recorded: "a transaction id that does not exist" and
  "a select after the commit with the old id").
- The `memory` catalog of Trino refuses a write in a transaction with
  `AUTOCOMMIT_WRITE_CONFLICT: Catalog only supports writes using autocommit`.
  The transaction then fails, and each later statement of it fails with
  `TRANSACTION_ALREADY_ABORTED` (recorded: "an insert in a transaction", "a
  create table in a transaction" and "a select after the failed insert").
- The `memory` catalog of Presto accepts a write in a transaction. A `ROLLBACK`
  does not undo it: the row was still in the table after the rollback
  (recorded: "an insert in a transaction" and "a select after the rollback").
  The connector decides this, and not the protocol. The dbrun images have no
  catalog with transactions that work.

These facts were measured with the integration tests of the driver on
2026-10-07:

- A `DELETE` of the `nextUri` of a query that runs in a transaction aborts the
  transaction on Trino: the next `COMMIT` fails with
  `TRANSACTION_ALREADY_ABORTED`. `QueryRow` reads one row and closes the rows
  before the server sees the last, empty page, so the driver reads up to four
  small pages in place of the `DELETE` when the connection holds a
  transaction, and sends the `DELETE` only for a query that is larger than that
  (see Open questions).
- Neither `memory` catalog makes a transaction that works, so the tests hold
  the behavior that Transactions names above, and the driver adds nothing to
  it.

These facts come from the sources, and are not measured:

- trino-go-client sends `START TRANSACTION` with `NONE` and keeps the id in the
  connection (trino-go-client, `BeginTx`, and issue 181).
- presto-go-client v2 omits the header `NONE`, and `BeginTx` fails with
  `INCOMPATIBLE_CLIENT` (presto-go-client issue 101).

## Errors

These facts were recorded on each release:

- A statement that fails sends HTTP 200, and a page with `error`. The page is
  the first page or a later page. A failed query ends with `stats.state` of
  `FAILED`, and with no `nextUri` (recorded: "an error before any row: syntax").
- `error` holds `message`, `errorCode`, `errorName`, `errorType`,
  `errorLocation` when the error has a place in the text, and `failureInfo`.
  `failureInfo` holds the Java class, the message and a stack of 20 to 40
  lines, which a client must not show. Presto adds `retriable`. Trino has
  `errorInfo` inside `failureInfo` (recorded: "an error before any row: syntax"
  and "an error before any row: unknown table").
- The names and the numbers are different between the flavors. A table that
  does not exist is `TABLE_NOT_FOUND` with the code 46 on Trino, and
  `SYNTAX_ERROR` with the code 1 on Presto. A column that does not exist is
  `COLUMN_NOT_FOUND` on Trino and `SYNTAX_ERROR` on Presto (recorded: "an
  error before any row: unknown table" and "an error before any row: unknown
  column"). The `errorName` that both flavors share is `SYNTAX_ERROR`,
  `DIVISION_BY_ZERO`, `INVALID_CAST_ARGUMENT`, `NUMERIC_VALUE_OUT_OF_RANGE`,
  `NOT_SUPPORTED`, `USER_CANCELED`, `EXCEEDED_TIME_LIMIT`,
  `INVALID_SESSION_PROPERTY`, `UNKNOWN_TRANSACTION` and `NOT_IN_TRANSACTION`
  (recorded: the requests of item 6, item 11 and "a statement with an unknown
  session property"). The `errorType` of every error that was recorded is
  `USER_ERROR`. The other types are not measured (Trino documentation, not
  read).
- An error after some rows arrives as a page with the rows, and then a page
  with `error`. 255 rows arrived and then `DIVISION_BY_ZERO` (recorded: "an
  error after some rows"). A bad cast late in the result gave the same
  (recorded: "an error after some rows: a bad cast late").
- A statement that exceeds a limit of the session fails with
  `EXCEEDED_TIME_LIMIT`, after the first pages (recorded: "an error from a
  limit of the session").
- An error of the protocol has the status of HTTP and a plain text body on
  Trino, and an HTML page or plain text on Presto. HTTP 400 is an empty
  statement (`SQL statement is empty`). HTTP 404 is a query or a token that
  the server does not know. HTTP 405 is `GET /v1/statement`. HTTP 410 is a
  `nextUri` that the client read before (recorded: "an empty statement", "a poll
  of a query that does not exist", "a GET of the statement endpoint" and
  "reread: poll four after poll five").
- A user that is missing gives HTTP 401 with `Basic authentication or
  X-Trino-Original-User or X-Trino-User must be sent` on Trino, and HTTP 400
  with `User must be set` on Presto (recorded: "no user").
- A password over HTTP gives HTTP 401 with `Password not allowed for insecure
  authentication` and `WWW-Authenticate: Basic realm="Trino"` on Trino.
  Presto ignores the `Authorization` header (recorded: "a wrong password" and
  "a wrong password with the user header").
- A limit on the rate, HTTP 429, and HTTP 502, 503 and 504 did not happen in
  any request. They are not measured.
- A request that did not reach the server fails at the connection, before any
  answer.

These facts come from the sources, and are not measured:

- trino-python-client retries a request on HTTP 429, 502, 503 and 504, for a
  `GET` and a `POST`, and it waits for the header `Retry-After` of a 429
  (trino-python-client, `client.py`). DeepSeek said that 503 and 504 need
  a retry for a `GET` of a `nextUri` only, and none for the `POST`. The
  source retries both.
- trino-go-client retries a request on transient errors, and it never sends a
  `POST` again after the server read it, and the `X-Trino-*` headers go to the
  host that a redirect names, which issue 207 reports (trino-go-client).

## Cancellation and timeouts

These facts were recorded on each release:

- A client cancels with `DELETE` on the `nextUri` that it holds. The answer is
  HTTP 204. The query then has the state `FAILED` with the error
  `USER_CANCELED`, and the next `GET` of the `nextUri` answers a page with
  that error (recorded: "cancel: delete the next uri", "cancel: the state
  after the cancel" and "cancel: poll after the cancel").
- A client cancels with `DELETE /v1/query/<id>` too. The answer is HTTP 204,
  and the state is the same. The answer is HTTP 204 for a query that has ended
  and for a query that does not exist (recorded: "cancel by the id: delete
  the query", "cancel by the id: the state after", "cancel by the id: delete a
  query that has ended" and "cancel by the id: delete a query that does not
  exist").
- `GET /v1/query/<id>` answers the state of a query, with a body of tens of
  kilobytes. The list `GET /v1/query?state=running` answers the queries that
  run on Trino (recorded: "cancel: the state before the cancel" and "the
  list of queries that run").
- The server does not stop a query when the client stops reading. A query
  that the client left after three polls was `RUNNING` 20 seconds later
  (recorded: "abandon: the state 20 seconds after the last poll"). The server
  stops it after its own timeout of the client, which is five minutes in the
  Trino documentation (not measured).
- Another user can read the state and cancel the query of the first user. The
  servers have no access control (recorded: "owner: another user reads the
  state", "owner: another user cancels the query" and "owner: the state after
  the other user").
- `X-Trino-Time-Zone` and the session properties are not timeouts. A limit on
  the time of a query is the session property `query_max_execution_time`
  (recorded: "an error from a limit of the session").
- `maxWait` on a poll and the effect of a slow client are not measured. The
  requests with `maxWait=1s` gave the same answers as the requests without it
  (recorded: "page size: poll 3").

These facts come from the sources, and are not measured:

- trino-go-client sends `DELETE /v1/query/<id>` when the rows close early, and
  gives each query a timeout of 10 hours when the context has no deadline
  (trino-go-client, `driverRows.Close` and `DefaultQueryTimeout`).
- trino-python-client sends `DELETE` to the `nextUri` (trino-python-client,
  `cancel`).

## Statements

These facts were recorded on each release:

- One request holds one statement. `SELECT 1; SELECT 2` fails with a syntax
  error, and so does a semicolon at the end, with or without a comment after it
  (recorded: "two statements", "a trailing semicolon" and "a statement with a
  trailing semicolon and a comment"). A semicolon inside a string is part of the
  string (recorded: "a statement with a semicolon in a string").
- A comment with `--` at the end, a block comment at the start, a block
  comment between tokens and a line comment with a new line work (recorded:
  "a trailing line comment", "a leading block comment", "a comment between
  tokens" and "a line comment with a newline").
- An empty statement fails with HTTP 400. A statement of white space fails
  with a syntax error (recorded: "an empty statement" and "a statement of
  white space").
- A column can have a repeated name or no name (recorded: "columns with a
  repeated name and an unnamed one").
- `USE <catalog>.<schema>`, `SET SESSION`, `RESET SESSION`, `SET ROLE` and
  `SET TIME ZONE` change the session. They do it in the response headers and
  the client sends them back, so they change nothing for a request that sends
  no header (recorded: "use", "a statement that sets a session property", "a
  statement that resets a session property" and "set time zone").
- `SET SESSION query_max_run_time = '5m'` is `X-Trino-Set-Session:
  query_max_run_time=5m`, and `SET TIME ZONE 'Asia/Jakarta'` is
  `X-Trino-Set-Session: time_zone_id=Asia%2FJakarta`. The client sends them as
  `X-Trino-Session`. A property that does not exist fails with
  `INVALID_SESSION_PROPERTY` (recorded: "a statement with the session property
  in the header", "set time zone" and "a statement with an unknown session
  property").
- `X-Trino-Time-Zone: Asia/Jakarta` sets `current_timezone()` (recorded: "a
  statement with the time zone header").
- `SET PATH` and `SET SESSION AUTHORIZATION` fail with `not supported by client`
  on Trino, unless the request sends the capability `PATH` or
  `SESSION_AUTHORIZATION`. `SET PATH` then works, and `SET SESSION
  AUTHORIZATION alice` fails with `User trino cannot impersonate user alice`.
  Presto has neither statement (recorded: "set path", "set path with the
  capability" and "set session authorization with the capability").
- `EXPLAIN`, `SHOW CATALOGS`, `SHOW SCHEMAS`, `SHOW TABLES`, `SHOW COLUMNS`,
  `DESCRIBE`, `SHOW CREATE TABLE`, `SHOW STATS` and `SHOW SESSION` work. A
  query of `information_schema.columns` works. `ANALYZE` fails on `memory`
  (recorded: the requests whose names start with `schema:`).
- The DDL that `memory` accepts is `CREATE TABLE`, `CREATE TABLE AS SELECT`,
  `CREATE VIEW`, `CREATE SCHEMA`, `ALTER TABLE ... RENAME TO` and
  `INSERT ... SELECT` on both flavors. Trino also accepts `NOT NULL`,
  `COMMENT ON`, `ADD COLUMN` and `RENAME COLUMN`, and Trino 483 accepts
  `DEFAULT`. Presto accepts `DEFAULT` and then stores NULL in place of the
  default. Presto accepts `CREATE MATERIALIZED VIEW`, and Trino refuses it.
  Both refuse `PRIMARY KEY`, `FOREIGN KEY`, `UNIQUE`, `CREATE INDEX`,
  `DROP COLUMN`, `INSERT ... ON CONFLICT` and `MERGE` (recorded: the requests
  whose names start with `schema:` and `crud:`).

## Principals

- No release has an ordinary user. The user is the name in `X-Trino-User` or
  `X-Presto-User`, and the servers accept any name. The `dbrun` images
  set up no authentication, and `dbrun` makes only the administrator `trino`
  or `presto`, with no password (recorded: "another user in the header" and
  "another user reads the table"). `SELECT current_user` gives the name of the
  header.
- Trino takes the user from the `Authorization` header too, when the password
  is empty: `Basic dHJpbm86` gave the user `trino` (recorded: "a user in basic
  authentication with an empty password"). Presto does not (recorded: the same
  request).
- Roles do not work. `CREATE ROLE` fails with `System roles are not enabled` on
  Trino and `This connector does not support roles` on Presto. `SET ROLE ALL`
  answers a header and does nothing (recorded: "create role", "show roles" and
  "set role").
- The version needs no privilege. `GET /v1/info` answers it with no header
  (recorded: "the info of the server"). `SELECT version()` works on Trino and
  fails on Presto with `Function version not registered` (recorded: "the
  version function"). `SELECT node_version FROM system.runtime.nodes WHERE
  coordinator = true` answers `476`, `483` and `0.299-7d50721` (recorded: "the
  version of the coordinator").

## Flavors

Trino and Presto share the protocol, and these facts differ. Each fact is
recorded on the requests that Requests, Responses, Types, Parameters,
Transactions, Errors and Statements name.

| Fact | Trino 476 and 483 | Presto 0.299 |
| --- | --- | --- |
| Header prefix | `X-Trino-` | `X-Presto-` |
| Version in `GET /v1/info` | `476`, `483` | `0.299-7d50721` |
| `SELECT version()` | the version | fails, `Function version not registered` |
| Version in `system.runtime.nodes` | `node_version` is `476` | `node_version` is `0.299-7d50721` |
| `nextUri` | the token in the path | the number in the path, the token in `?slug=` |
| Type signature | `arguments` with `kind` of `LONG`, `TYPE`, `NAMED_TYPE` | adds `typeArguments` and `literalArguments`, and `kind` of `LONG_LITERAL`, `TYPE_SIGNATURE`, `NAMED_TYPE_SIGNATURE` |
| Type text | `decimal(38, 0)`, `INTERVAL YEAR TO MONTH` | `decimal(38,0)`, `interval year to month` |
| Array, map and row | JSON values | strings of JSON text |
| Time precision | 3 digits, or 12 with `PARAMETRIC_DATETIME` | 3 digits |
| `time with time zone` | `12:34:56.789+05:30` | `12:34:56.789 +05:30` |
| `NUMBER` and `VARIANT` | Trino 483 only, with a capability | none |
| DDL answer | no column, no row | the column `result` with `true` |
| First rows | on the page after the first page with columns | on the page with the columns, or the next |
| Error `retriable` | none | yes |
| Error names | `TABLE_NOT_FOUND`, `COLUMN_NOT_FOUND` | `SYNTAX_ERROR` for each |
| Protocol errors | plain text | HTML or plain text |
| Redirect from `/` | HTTP 303 | HTTP 307 |
| No user | HTTP 401 | HTTP 400, `User must be set` |
| Password | HTTP 401 over HTTP, user from `Authorization` | ignored |
| `targetResultSize` | ignored | smaller pages |
| `EXECUTE IMMEDIATE` | works | fails |
| `LIMIT ?` | works | fails |
| `SET PATH`, `SET TIME ZONE`, `SET SESSION AUTHORIZATION` | known to the server | fail |
| `current_catalog`, `current_schema` | work | fail |
| `WITH RECURSIVE` | works | fails |
| `COMMENT ON`, `NOT NULL` | work in `memory` | fail |
| `TRUNCATE`, `ADD COLUMN`, `RENAME COLUMN` | work in `memory` | refused by `memory` |
| `DEFAULT` | 483 stores it, 476 fails | accepts it and stores NULL |
| Materialized view | refused by `memory` | works |
| Write in a transaction | refused by `memory` | accepted, and `ROLLBACK` keeps it |
| `partialCancelUri` | the host of the request | the internal address of the server |
| Insert of a value of another numeric type | coerced | refused, so a caller casts |
| `TIME` literal | up to 12 digits | 3 digits |
| `TIME WITH TIME ZONE` at offset zero | `+00:00` | `UTC` with no space |
| Row field names of a stored table | `row(a integer)` on 476, `row("a" integer)` on 483 | `row("a" integer)` |

The server says which flavor answered:

- `GET /v1/info` gives `nodeVersion.version`. Trino writes a number, such as
  `476`. Presto writes a number, a dash and a commit, such as `0.299-7d50721`.
  The key `environment` was `docker` on Trino and `test` on Presto, which the
  image decides, and a driver cannot use it (recorded: "the info of the
  server").
- `SELECT version()` works on Trino only (recorded: "the version function").
- The `nextUri` of Presto has `?slug=`, and the `nextUri` of Trino has not
  (recorded: "a statement").
- A request with the header of the wrong flavor fails: Presto answers `User
  must be set` to `X-Trino-User` (measured with `curl` on presto-0.299 on
  2026-10-07). A driver that sends both headers works on both flavors, and
  that is not measured.
- dbmeta runs `SELECT version()` for Trino and `SELECT node_version FROM
  system.runtime.nodes WHERE coordinator = true LIMIT 1` for Presto
  (dbmeta, `models/trino/trino.go` and `models/presto/presto.go`). The second
  statement works on Trino too (recorded: "the version of the coordinator"),
  so it names no flavor by itself.

## Interfaces

The code writes this table, and a test makes sure that it is current.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and asks the server which product it is, once. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the user and the password. GET /v1/info needs neither. |
| `driver.SessionResetter` | yes | A connection keeps the catalog, the schema, the properties of the session and the prepared statements that the servers ask for, so ResetSession gives each caller the state of the DSN (D175). |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a decimal, the types of the root package, a UUID, a list and a map, which the driver writes as a literal of their own type (D175). |
| `driver.QueryerContext` | yes | The driver sends each argument as a literal of EXECUTE name USING, on a statement that it names in the prepared-statement header (D175). |
| `driver.ExecerContext` | yes | Exec reads the result to its end, and RowsAffected is the updateCount of the server, if it sent one. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx sends START TRANSACTION with the transaction headers, and keeps the id that the server answers (D175). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result. |
| `driver.RowsColumnTypeScanType` | yes | The columns name their type, with its arguments, and each type has one Go type (D135 and D175). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of a column, in upper case, without its arguments, such as BIGINT or TIMESTAMP WITH TIME ZONE. |
| `driver.RowsColumnTypeLength` | yes | A char and a varchar have the length in their type, and a varchar with none and a varbinary give the largest value. |
| `driver.RowsColumnTypeNullable` | yes | The columns do not say whether they can be NULL, and every type can be. |
| `driver.RowsColumnTypePrecisionScale` | yes | A decimal has its precision and its scale in its type. |
<!-- /dbimp:interfaces -->

## Faults

These faults are known in the clients that `usql` uses now. This driver must
not repeat them. Each one is from the sources, and is not measured:

- trino-go-client followed a redirect with the `X-Trino-*` headers intact, and
  a status of 301, 302 or 303 turned the `POST` into a `GET` that dropped the
  body. The server can then run nothing, or the wrong thing (trino-go-client
  issue 207, closed).
- trino-go-client wrote `Numeric("NaN")` and `Numeric("1_000")` into the query
  as a literal that the server reads as a column name (issue 202, closed).
- trino-go-client wrote `TIME '... Z'` for a time with a zone of UTC, which
  Trino cannot read (issue 206, closed), and it lost the sign of a negative
  duration under one second (issue 203, closed).
- trino-go-client dropped a password silently on a URL that is not `https`
  (issue 205, closed), and put the named argument `accessToken` into the text
  of the query (issue 204, closed).
- trino-go-client did not track the id of a transaction, so `START TRANSACTION`
  and a write ran as separate transactions (issue 181, closed).
- trino-go-client returned an empty `[]byte` for a NULL `VARBINARY` (issue 166,
  closed), and a value set by one caller stayed on a connection of the pool
  (issue 229, closed).
- trino-go-client did not send `X-Trino-Time-Zone`, so a timestamp with no zone
  can have the wrong zone (issue 183, closed).
- presto-go-client decoded a `BIGINT` through `float64`, so
  `9223372036854775807` became `-9223372036854775808` on `linux/amd64` (issue
  102, open).
- presto-go-client `BeginTx` failed on Presto because it omitted the header
  `X-Presto-Transaction-Id: NONE` (issue 101, open).
- presto-go-client did not scan an array, a map or a row on a live server
  (issue 103, open), because the server sends them as strings.
- trino-go-client returns a `decimal` and an interval as a `string`, and a
  `char` and a `varchar` as a `string`. D135 wants an `*apd.Decimal` and a
  `dbimp.Interval` (trino-go-client, `convertScalar`).
- Neither `usql` file sets a `Version` statement, so `usql` cannot show the
  version of the server (usql).

## Second opinions

Each lead from step 5a, step 7 and step 8a is below. A lead that the server did
not show to be true stays "not measured", with the model that gave it.

Step 5a, the survey (Gemini and DeepSeek, 2026-10-07):

- Gemini said that `UPSERT` does not exist and `MERGE` replaces it. The
  server refused `INSERT ... ON CONFLICT` with a syntax error, and `MERGE`
  with `This connector does not support modifying table rows` (recorded:
  "crud: upsert" and "crud: merge").
- Gemini said that `TRUNCATE` is supported, and DeepSeek said that it is not a
  Trino statement. Trino ran it on `memory`, and Presto refused it (recorded:
  "crud: truncate").
- Both said that a primary key, a foreign key, a unique constraint and an
  index do not exist. The server refused each one (recorded: the requests whose
  names start with `schema:`). Gemini said that `DEFAULT` works in Trino, which
  is true on 483 and false on 476, and Presto stores NULL (recorded: "schema:
  select the default value").
- Gemini said that `EXECUTE IMMEDIATE` is Trino only, which is true (recorded:
  "feature: execute immediate").
- Gemini said that a geometry is base64 WKB. The server sends WKT text
  (recorded: "types: geometry"). Gemini said that Trino has no `NUMBER` and
  no `VARIANT`. Trino 483 has both, with a capability (recorded: "types:
  number with the capability" and "types: variant with the capability").
- DeepSeek said that a `DECIMAL` is a JSON number. The server sends a string
  (recorded: "types: boolean, integers, floats, decimals, text and bytes").
- Gemini said that Trino compresses data with `Accept-Encoding`, for the query
  data. The server compresses the JSON pages with gzip, and the header
  `X-Trino-Query-Data-Encoding` needs spooling (recorded: "a statement with
  gzip" and "a statement with a query data encoding that the server lacks").
- Both models were asked in separate conversations. DeepSeek answered after
  several timeouts, one question at a time.

Step 7, the leads (Gemini, and DeepSeek):

- Gemini said that `DEALLOCATE PREPARE` answers `X-Trino-Clear-Prepare`. It
  answers `X-Trino-Deallocated-Prepare` (recorded: "deallocate").
- Gemini said that the header `X-Trino-Max-Wait` delays a poll and
  `X-Trino-Max-Size` limits a page. Neither changed an answer on either flavor
  (recorded: "page size by header: poll 5" and "page size by header: poll 2").
  DeepSeek said that `X-Trino-Max-Size` sets the size of a page, and that
  `maxWait` is a query parameter. The header is wrong. The query parameter
  `targetResultSize` works on Presto only, and `maxWait` is not measured
  (recorded: "page size: poll 3" and "page size: poll 5").
- Gemini said that a `nextUri` can be relative. Each `nextUri` that was
  recorded is absolute (recorded: "a statement").
- Gemini said that `COMMIT` and `ROLLBACK` answer `X-Trino-Clear-Transaction-Id:
  true`. They do (recorded: "commit" and "rollback").
- Gemini said that a `GET` that waits for data can run while another request
  sends `DELETE` to the same `nextUri`. This is not measured, because the images
  have no query that waits with no output.
- DeepSeek said to retry a 503 or a 504 for a `GET` of a `nextUri` and never for
  the `POST`. Nothing caused those statuses, and the clients that were read
  retry both (not measured).
- Gemini said that the spooling protocol sends a `nextUri` that points at cloud
  storage. The images do not enable spooling, and the header
  `X-Trino-Query-Data-Encoding` changed nothing (recorded: "a statement with a
  query data encoding that the server lacks"). Spooling is not measured.

Step 8a, the review of the type table (Gemini and DeepSeek, 2026-10-07):

- Gemini agreed with every row. It said to keep the padding of a `CHAR`, to
  keep the keys of a `MAP` as strings, to give `nil` for a JSON `null` and for
  a SQL NULL, and to cut the fraction to nanoseconds. It said to return an
  error for a zone name that Go cannot load. It said that `IPADDRESS` is
  `other`, and that the sketches are `binary`.
- DeepSeek agreed with every row except two. It said that a fraction cut to
  nanoseconds breaks the rule that a value that cannot be decoded is an error,
  and it asked for an error or a type with more precision. It said that
  `NUMBER` is not a `DECIMAL`, because it holds `1E+1000` and `NaN`, and it
  asked for a type of its own or the text. It said that a JSON `null` and a
  SQL NULL are ambiguous when both are `nil`. It agreed with the padding of a
  `CHAR` and with the string keys of a `MAP`.
- The server showed this about the answers: a TIME with 12 digits arrives only
  with the capability, so a driver that never sends the capability gets 3
  digits and nothing to cut. A zone name arrives as a name or as an offset.

## Open questions

These questions wait for Ken, except the second and the third:

1. The `memory` catalog of each `dbrun` image refuses `UPDATE`, `DELETE` and
   `MERGE`, and Presto refuses `TRUNCATE`. DRIVER.md says to stop and ask Ken
   when a server refuses one of insert, select, update and delete. Does the
   driver go on with `INSERT`, `SELECT` and `CREATE TABLE AS SELECT` only, or
   does `dbmeta` first add a catalog that supports row changes, such as
   Iceberg with a file metastore, for each release?
2. The recorder could not follow an absolute `nextUri`, nor keep a response
   header. The session changed `dbimptest/cmd/record/main.go` on 2026-10-07:
   a `path` or a followed address that is an absolute URL goes to the server
   of the script, and a capture of `header:<name>` keeps a header of the
   response. This question is closed.
3. `testdata/trino/` holds 4594 recorded exchanges, about 33 megabytes,
   because `follow` records each poll. They compress to 1.7 megabytes, against
   1.1 for `testdata/influxdb/`, and the whole pack of the repository is 8.6
   megabytes, so the size is kept. This question is closed.
4. Picoseconds. Ken decided on 2026-10-07 that the driver drops digits beyond
   the ninth only when they are all zero, and fails for a value with a
   non-zero digit there (D175).
5. A Trino 483 `NUMBER` is an `*apd.Decimal` (D175).
6. A JSON `null` and a SQL NULL are both `nil` (D175).
7. `IPADDRESS` is the kind `other`, with a string (D175).
8. A zone name that Go cannot load, in a `TIMESTAMP WITH TIME ZONE`, is an
   open question for the driver tests, and is not decided.
9. The driver decodes the `ARRAY`, `MAP` and `ROW` of Presto with the type of
   the column, so both flavors give the same Go values (D175).
10. The decisions of step 9 are D175.

These questions came with the driver, and wait for Ken:

11. A `DELETE` of the `nextUri` aborts a transaction (see Transactions), and
    D175 says to send it when the rows close early. The driver reads up to four
    small pages in place of the `DELETE` when its connection holds a
    transaction, so that `QueryRow` in a transaction keeps the transaction.
    Does D175 change to say so, or does the driver always send the `DELETE`?
12. `QueryContext` reads up to the first row, so that an error that comes before
    any row is the error of `QueryContext`, as step 12 of DRIVER.md says. D175
    says only that the driver polls `nextUri`.
13. D175 gives `WithParameter` no meaning, because the servers take no body of
    keys. The driver sets a property of the session with it, as the keys
    `session.<name>` of the DSN do. `WithDatabase` sets the catalog, and
    `WithSchema` sets the schema.
14. The driver sends the capabilities `PARAMETRIC_DATETIME`, `NUMBER` and
    `VARIANT` to every Trino, and none to Presto. D175 says to send `NUMBER`
    and `VARIANT` to a release that has them. A server does not name a
    capability that it does not know (see Types), so the result is the same,
    and the driver needs no version to decide.
15. A zone name that Go cannot load, in a `TIMESTAMP WITH TIME ZONE`, is an
    error of the row, because the value cannot be held without the zone
    (question 8). The driver imports no zone data, so a host with none fails
    for each such value.
16. A zero `dbimp.Interval` is written as an `INTERVAL DAY TO SECOND` (see
    Parameters), so it cannot go into a `YEAR TO MONTH` column.
17. The default `source` of a statement is `dbimp`, and the default time zone
    is the one of the server. D175 names the keys and no default.
18. A `ROW` value is a `[]any` in the order of its fields, so a caller cannot
    read the field names from the value (D135 and D137). Ken decided on
    2026-10-07 to do nothing now, and to remove the `Row` type that held a
    row argument. The names are in the type name of the column, such as
    `row(a integer, b varchar)`, from `ColumnTypeDatabaseTypeName`. If a
    caller needs the names later, there are three options:
    - Do nothing more. A caller parses the type name of the column.
    - Add a helper such as `trino.FieldNames(rows, i)` that parses that type
      name and returns the names. It adds an export and changes no scanned
      value. This is the option to try first.
    - Add a named scan type, as the official client does with its `Row`
      struct. A caller who wants the names scans into it. It breaks the rule
      of one Go type for each kind (D135 and D137), so it needs a decision
      from Ken first.
    A row argument has no type either. A `[]any` is an `ARRAY`, so a caller
    who needs to send a `ROW` writes `CAST(JSON_PARSE(?) AS row(...))` and
    passes JSON text.

## Integration tests

The integration tests of the driver read `TRINO_DSN`, which is the `url` of
`dbrun`, and skip when it is empty. They ran on 2026-10-07, one server at a
time, with `-race`, on a fresh container of each release:

| Release | Passed | Skipped | Failed |
| --- | --- | --- | --- |
| `trino-476` | 104 | 24 | 0 |
| `trino-483` | 104 | 24 | 0 |
| `presto-0.299` | 104 | 24 | 0 |

A count is a test or a subtest. Each skip names the flavor or the release that
its entry is about. The servers have no ordinary user, so the tests run as the
administrator, and `TestIntegrationUsers` runs as the user `alice`, which the
servers take from the header. One container of Presto 0.299 exited with the
code 3 after about eight runs of the tests, and a fresh container ran them
clean. The cause is not known.

The `memory` catalog stores no column of the type `unknown`, so the round trip of
that type stores a `bigint` and selects `NULL`, which has the type `unknown`.
The round trip skips the update and the delete, because the catalog refuses
them (D175), and drops the table at the end.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-10-07 from the staged code. A fact of Couchbase comes
from [COUCHBASE.md](COUCHBASE.md), and a fact of Trino and Presto from the
sections above.

### The server

| | Couchbase | Trino and Presto |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /v1/statement` with the SQL text as the body, then `GET` of each `nextUri` (Requests) |
| Database | The key `query_context` of the body | The headers `X-Trino-Catalog` and `X-Trino-Schema`, or the same with `X-Presto-` (Requests) |
| Language | SQL++, which is close to SQL | SQL. One statement for each request, with no semicolon at the end (Statements) |
| DDL | In SQL++ | In SQL. The catalog decides what works: `memory` makes tables and views, and refuses `UPDATE`, `DELETE` and `MERGE` (Statements) |
| Parameters | `?`, `$1` and `$name` | `?` only, in a prepared statement that a header names, and `EXECUTE name USING` the literals (Parameters) |
| Framing | One body for the whole result, which does not page | A series of pages. Each page is a JSON object with the member `nextUri`, and the last has none (Responses) |
| Columns | `signature`, before the first row | `columns`, as a list of `name`, `type` and `typeSignature`, before the first row (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. A name can repeat (Responses) |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 and a page with `error`, before any row or after some. An error of the protocol has the status of HTTP (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a type for each column. A decimal, a time, a UUID, a binary value and a JSON value are strings, and Presto sends an array, a map and a row as strings of JSON text (Types) |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves, and `DELETE` of the `nextUri` stops it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `START TRANSACTION` with the header `NONE`, and the id in a header. The catalog decides what a transaction does (Transactions) |
| Authentication | Basic, or `creds` in the body | The user in a header, and basic authentication when a server asks for a password (Principals) |
| Default port | 8093, or 18093 with TLS | 8080, or 8443 with TLS |

The differences that a caller sees:

- A result comes in pages, and the driver reads each one token at a time, so
  no result is held in memory (D175 and D25).
- The servers hold no state for a client, so the driver keeps the catalog, the
  schema, the properties of the session and the statements of `PREPARE` on the
  connection, and `ResetSession` clears them (D175).
- A write in a transaction works on Presto and fails on Trino, and a
  `ROLLBACK` on Presto does not undo it. The `memory` catalog decides both
  (D175).
- `UPDATE`, `DELETE` and `MERGE` fail on every release that was tested, because
  the only catalog that writes refuses them (D175).

### The driver

| | `couchbase` | `trino` |
| --- | --- | --- |
| Size, without tests, on 2026-10-07 | About 1300 lines in 8 files | About 2800 lines in 8 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Catalog`, `Schema`, `Source`, `TimeZone`, `Timeout`, `Session`, `Flavor` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithSchema`, `WithTimeZone` and `WithSource`, through `WithOptions` or an argument (D109). `WithTimeout` sets `query_max_execution_time`. `WithReadonly(true)` gives `dbimp.ErrNotSupported`. `WithParameter` sets a property of the session. `WithDatabase` sets the catalog |
| Arguments | Sent to the server as `args` and `$name` | Written as the literals of `EXECUTE name USING`, from the Go type of each argument. A named argument, `NaN` in a decimal and an interval of months and days are refused (`trino/literal.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own for the pages, which reads one page member at a time and decodes each value by the type of its column (`trino/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypeLength` and `ColumnTypePrecisionScale`, from the type signature. Every column can be NULL |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: the types of the root package for a date, a time, a timestamp with no zone and an interval, `*apd.Decimal`, `uuid.UUID`, and a list or a map of decoded values for a container, on both flavors (D135 and D175) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `updateCount`, and `dbimp.ErrNotSupported` when the server sent none. `LastInsertId` always gives `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` sends `START TRANSACTION` with the isolation level and `READ ONLY`, and keeps the id (D175) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | `ResetSession` gives the state of the DSN back, because the connection keeps state of the session on the client (D175) |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The driver sends `DELETE` of the `nextUri` when the rows close early and when the context ends, with a limit of 5 seconds. The rows keep the context of the query, which is an exception to hard rule 4 (D175) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Name, Type, Code, Message, Line, Column}`, which unwraps to `*dbimp.StatusError` for an error of the protocol |
| Authentication | Basic | Basic, the user header, and no redirect, so the secret and the user go to the host of the DSN only. Each `nextUri` uses the host of the DSN (D175) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, `Config`, `Flavor` and the two names of the flavors |

The differences that a caller sees:

- A value keeps its type, a time, a decimal, a UUID and an interval too, where
  Couchbase gives JSON shapes (D135 and D175).
- A container is a `[]any` or a `map[string]any` on both flavors, even though
  Presto sends text (D175).
- A time with more than nine digits of fraction fails for a value that has a
  digit beyond the ninth that is not zero, and a zone that Go cannot load
  fails the row (D175 and Open questions).
- The connection keeps state, so `USE` and `SET SESSION` work, and `ResetSession`
  is not a guard (D175).
- `WithParameter` sets a property of the session, where Couchbase sets a key of
  the body, because the servers take no body of keys (Open questions).
- A transaction on the `memory` catalog of Trino cannot write, and on Presto a
  `ROLLBACK` keeps a write (D175).
- The rows of a query in a transaction read up to four pages when they close
  early, where D175 says to send the `DELETE` (Open questions).
