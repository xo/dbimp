# SurrealDB

This file holds what is known about the HTTP interface of SurrealDB, for the
second driver, `github.com/xo/dbimp/surrealdb`. W8 in
[BACKLOG.md](BACKLOG.md) is the work, and it follows
[DRIVER.md](DRIVER.md). The headings are the template of that file.

Each fact says how it was measured. "Recorded" means that an exchange under
`testdata/surrealdb/` holds it, for 2.7.0, 3.1.6, 3.2.4 and 3.3.0, as the
administrator and as the ordinary user, recorded on 2026-09-27 from the
script `testdata/surrealdb/requests.json` with `dbimptest/cmd/record`. A fact
that only some releases or one principal show names them. "Measured with
curl" means that this session sent the request by hand on 2.7.0 and 3.3.0 on
2026-09-27, and no file holds it. "Tested" means that an integration test in
`surrealdb/integration_test.go` holds it, and passed on 2.7.0, 3.1.6, 3.2.4
and 3.3.0 as both principals on 2026-09-27. "The Go SDK" is
`github.com/surrealdb/surrealdb.go` v1.7.0, and "the JavaScript SDK" is the
npm package `surrealdb` 2.0.8, both read on 2026-09-27. A fact marked "not
measured" is a lead, not a fact.

## Summary

- The product is SurrealDB, with the query language SurrealQL.
- `dbrun` names four releases, from dbmeta D103: `surrealdb-2.7.0` and
  `surrealdb-3.3.0` in the Tested tier, and `surrealdb-3.1.6` and
  `surrealdb-3.2.4` in the Nightly tier (read from `dbrun list --json` at
  dbmeta commit `78f1e44` on 2026-09-27). Each is the image
  `docker.io/surrealdb/surrealdb`, which the vendor builds. The server stores
  its data in RocksDB.
- It meets R, H and S, and it is P1 and the second driver (W8).
- `dburl` has the scheme `surrealdb`, with the aliases `sr`, `sur` and
  `surreal`, from D47 and D48. dburl `v0.34.0` releases it. `usql` imports
  this driver in `usql/drivers/surrealdb`, from usql commit `54c12a4`, and
  requires dbimp `v0.2.0`. No release of `usql` holds it yet (read on
  2026-09-28).
- The survey of step 5a is `testdata/surrealdb/features.json`. Each entry is
  settled against 3.3.0 and recorded on the four releases, as both
  principals, and each one names the integration test that holds it (step
  14a).

## Requests

- A query is `POST /rpc` with a body of the JSON-RPC form
  `{"method": "query", "params": [text, vars]}`, and `vars` is optional
  (recorded). The request needs no `id`, and the response has one only when
  the request had one.
- The body and the response are CBOR when `Content-Type` and `Accept` are
  `application/cbor`, and JSON when they are `application/json` (recorded).
- `POST /sql` takes the text as a plain body, and answers the array of
  results alone (recorded). It binds each key of its query string as a
  variable. 2.7.0 read `?x=5` as the number 5, and 3.x read it as the string
  `"5"` (recorded).
- `Surreal-NS` and `Surreal-DB` name the namespace and the database of the
  statement (recorded).
- Authentication is basic. The root user sends only its name and its
  password. A user defined on a database also sends `Surreal-Auth-NS` and
  `Surreal-Auth-DB`, which name where the user is defined. Each form refuses
  the other user with HTTP 401 (measured with curl, and by the `dbmeta`
  session on 2.7.0 and 3.3.0).
- `POST /signin` with `{"ns", "db", "user", "pass"}` returns a token for a
  database user (measured by the `dbmeta` session). A wrong password is HTTP
  401 (recorded).
- The RPC methods `ping` and `version` exist over HTTP (recorded), and
  `GET /health` answers HTTP 200 with no body and no credentials (recorded).
- A body of `/rpc` larger than 4 MiB is refused with HTTP 413. 3 MiB was
  taken (measured with curl). Gemini gave the lead.

## The DSN

- The URL is `surrealdb://user:pass@host:port/<namespace>/<database>` (D47
  and D48). The default port is 8000.
- `auth` is `root`, `namespace` or `database`, and the default is `root`
  (D51).
- `tls=true` makes the driver speak HTTPS on the same port (D48). Whether the
  server serves TLS on the port that it binds is not measured. Gemini and
  DeepSeek said that it does.
- `encoding` is `cbor` or `json`, and the default is `cbor` (D49).
- `dbrun dsn --json` prints the URL of each principal in the field
  `principals`, and the URL of the ordinary user holds `?auth=database`
  (dbmeta D103, read on 2026-09-27).

## Responses

- A response of `/rpc` is an object that holds `result`, an array with one
  entry for each statement, or `error`, an object with `code` and `message`
  for a failure of the call itself (recorded).
- Each entry of `result` holds `result`, the value of the statement,
  `status`, `OK` or `ERR`, and `time`. 3.x adds `type`, and an entry of 3.x
  with `ERR` adds `kind` and can add `details` (recorded).
- The server sorts the keys of every object, in JSON and in CBOR, so `result`
  arrives before `status`. `SELECT b, a, c` returns the keys `a`, `b` and `c`
  (recorded). No setting keeps the order of the projection (Gemini and
  DeepSeek, not measured).
- No list of columns arrives. A `SELECT` returns an array of objects, and
  each record has the fields that it holds, so two records can have
  different keys (recorded). `SELECT VALUE` returns an array of values, and
  `SELECT ... FROM ONLY` and `RETURN` return one value (recorded).
- So by D52 a statement whose values are objects gives the keys of the
  objects as its columns, whatever form it has. `SELECT VALUE v`, where `v`
  is an object, and `INFO FOR DB` each give the keys of the object as the
  columns. `SELECT v` gives one column `v` that holds the object (tested).
- The server sends the whole response at once, and does not page it. 3.3.0
  sent a `Content-Length`, and 2.7.0 sent chunks (measured with curl). A
  `SELECT` of 100000 records was one body of 3.4 MB (measured with curl), and
  5000 records were one body (recorded).
- 3.x refuses a function whose output is larger than 1 MiB, such as
  `array::range(0, 200000)`, with `ERR` (recorded). 2.7.0 has no such limit.
- The server compresses a response with gzip when the request asks for it
  (recorded).
- The server sent no redirect in any measurement.

## Types

In CBOR, the server tags each type that JSON has no form for (recorded on
each release):

- An integer is a CBOR integer, from -9223372036854775808 to
  9223372036854775807. A literal outside that range is a parse error.
- A float is a CBOR float. A float that is a whole number, such as `1f`,
  stays a float in CBOR, and JSON writes it as `1.0`.
- A decimal is tag 10 around its text, with at most 28 digits after the
  point: `1.23456789012345678901234567890dec` arrived as
  `1.2345678901234567890123456789`.
- A datetime is tag 12 around `[seconds, nanoseconds]`, from the year 1600
  to 9999 at least.
- A duration is tag 14 around `[seconds, nanoseconds]`, and the empty array
  for zero. `300y` arrived as 9460800000 seconds, which is longer than a
  `time.Duration` holds.
- A UUID is tag 37 around its 16 bytes.
- Bytes are a CBOR byte string.
- A record id is tag 8 around `[table, key]`. The key is a string, an
  integer, an array, an object or a UUID.
- A table name, such as `type::table('person')`, is tag 7 around its name.
- A set is tag 56 around an array on 3.x, and a plain array on 2.7.0. The
  server sorts the members of a set.
- A geometry is tags 88 to 94: 88 for a point `[x, y]`, 89 for a line, 90 for
  a polygon, 91 to 93 for the multiple forms, and 94 for a collection.
- A range is tag 49 around `[begin, end]`. Each bound is tag 50 when it is
  included, tag 51 when it is excluded, and null when it is open.
- NONE is tag 6 around null, and NULL is CBOR null. A field that a
  projection names and a record lacks is NONE: `SELECT a, b FROM [{a: 1}]`
  returns `b` as tag 6.
- A regex and a closure cannot be written in CBOR. The request fails with
  HTTP 400, "Found unsupported SurrealQL value being encoded into a CBOR
  value" on 2.7.0 and "Parse error" on 3.x.

In JSON, the same values lose their types (recorded):

- A decimal, a datetime, a duration, a UUID, a record id and a table are
  strings, such as `"dbimp_people:tobie"`. A record id whose table is a
  keyword is quoted, such as `` "`cancel`:y" `` (measured with curl on
  2.7.0).
- Bytes are an array of numbers, and NONE is null.
- 2.7.0 writes a range as the dump of a Rust enum, such as
  `{"beg": {"Included": {"Number": {"Int": 1}}}, ...}`, and 3.x writes
  `"1..5"`.
- 2.7.0 writes a closure as its syntax tree, and 3.x refuses it with the
  RPC error "Closure values cannot be converted to public value".
- A regex is a string, such as `"/a.b/"` on 3.x and `"a.b"` on 2.7.0.
- A file, `f'bucket:/a.txt'`, is a parse error on 3.x unless the
  experimental feature for files is on, and a parse error on 2.7.0.

The type table comes from the code, in step 10.

<!-- dbimp:types -->
| Wire type | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- |
| none | `nil (tag 6)` | `interface {}` | `` | yes |
| null | `nil` | `interface {}` | `` | yes |
| bool | `bool` | `interface {}` | `` | yes |
| int | `int64` | `interface {}` | `` | yes |
| float | `float64` | `interface {}` | `` | yes |
| decimal | `*apd.Decimal (tag 10)` | `interface {}` | `` | yes |
| string | `string` | `interface {}` | `` | yes |
| datetime | `time.Time in UTC (tags 0 and 12)` | `interface {}` | `` | yes |
| duration | `time.Duration (tags 13 and 14)` | `interface {}` | `` | yes |
| uuid | `uuid.UUID (tags 9 and 37)` | `interface {}` | `` | yes |
| bytes | `[]byte` | `interface {}` | `` | yes |
| array | `[]any` | `interface {}` | `` | yes |
| set | `[]any (tag 56)` | `interface {}` | `` | yes |
| object | `map[string]any` | `interface {}` | `` | yes |
| record | `RecordID (tag 8)` | `interface {}` | `` | yes |
| geometry | `map[string]any of GeoJSON (tags 88 to 94)` | `interface {}` | `` | yes |
| range | `string, such as 1..5 (tags 49 to 51)` | `interface {}` | `` | yes |
| table | `string (tag 7)` | `interface {}` | `` | yes |
<!-- /dbimp:types -->

## Parameters

- `/rpc` binds each key of `vars` as `$key`, with its type (recorded). A
  string with a quote, an integer, a float, a boolean, null and an object
  arrive unchanged.
- SurrealQL has named parameters only. `$1` is a parse error (recorded).
- A variable that no statement names is ignored, and a name that no variable
  gives is NONE (recorded).
- `LET $a = 5` in one statement sets `$a` for the later statements of the
  same request (recorded).

## Transactions

- `BEGIN`, `COMMIT` and `CANCEL` work inside the text of one request
  (recorded). A write inside a cancelled transaction is gone, and a failed
  statement makes the other statements of the transaction fail with `ERR`.
- 3.x returns an entry for `BEGIN` and for `COMMIT`. 2.7.0 returns none, so
  the same text has fewer entries on 2.7.0 (recorded).
- A transaction cannot span two requests. The RPC method `begin` does not
  exist over HTTP (recorded). `BEGIN` without `COMMIT` in the same request is
  `ERR`, "Missing COMMIT statement", and a header `Surreal-Session` did not
  join two requests (measured with curl). A `COMMIT` alone gave the error
  "unreachable logic" on 2.7.0, which is a fault of the server (measured
  with curl).
- The Go SDK refuses an interactive transaction over HTTP too.
- So the driver has no transactions (D54).

## Errors

- A parse error in `/rpc` is HTTP 200 with the RPC `error`, code -32000, and
  no statement runs (recorded).
- A parse error in `/sql`, and a body of `/rpc` that is not JSON, are HTTP
  400 with a JSON body of `code`, `details`, `description` and
  `information` (recorded).
- A statement that fails is HTTP 200 with `"status": "ERR"` and the message
  in `result`, and the other statements of the request still run (recorded).
  `THROW`, a `TIMEOUT` clause, a unique index, and a coercion of a field
  each give `ERR`.
- A table that does not exist is `ERR` on 3.x, and an empty result on 2.7.0
  (recorded).
- A wrong password is HTTP 401 with a plain text body, "The password did not
  verify" (recorded).
- A request with no namespace is `ERR`, "Specify a namespace to use"
  (measured with curl).
- The server sent no HTTP 429 in any measurement.
- The driver returns each of these as a `*ResponseError` that holds an
  `Error` with the code, the kind and the message, except the plain text of
  HTTP 401, which is a `*dbimp.StatusError` (D55). A statement that failed
  reads as `surrealdb: NotFound: The table 'dbimp_nothing' does not exist`
  (tested).

## Cancellation and timeouts

- The server stops a query when the client disconnects. `SLEEP 3s; CREATE
  cancel:x` was left after 1 second, and `cancel:x` did not exist 4 seconds
  later, on 2.7.0 and 3.3.0 (measured with curl, and recorded as item 7).
- No endpoint and no statement lists or stops a running query. `KILL` and the
  RPC method `kill` take the id of a live query, `SHOW QUERIES` is a parse
  error, and `GET /queries` is 404 (measured with curl). Gemini and DeepSeek
  said the same.
- The `TIMEOUT` clause stops a statement with `ERR` (recorded).
- The ordinary user is refused `SLEEP` (recorded).
- A changefeed shows the change of a new table at once on 2.7.0. On 3.3.0, a
  table defined and written in the same request showed no change, and none
  after 40 seconds, while a table whose name had been used before showed the
  changes of its earlier tables (measured with curl). Why is not measured.
  The integration test holds only that `SHOW CHANGES` runs on 3.x.

## Statements

- One request holds several statements, and the response has one entry for
  each, in order (recorded). The server splits the text itself, so the
  driver never splits it (D8).
- A comment with `--`, `#` or `/* */`, and a `$x` or a `;` inside a comment
  or a string, does not affect the statement (recorded).

## Principals

- The ordinary user of `dbrun` is `dbmeta_user`, an `EDITOR` on the database
  `dbmeta` of the namespace `dbmeta` (dbmeta D103).
- It can define and remove a table, a field, an index, an event, a function
  and a param, and it can run every CRUD statement of the survey (recorded).
- It is refused a user, a sequence, `SLEEP` and `INFO FOR ROOT`, with `ERR`
  and "IAM error: Not enough permissions to perform this action" (recorded).
- It reads the version with the RPC method `version`, as the administrator
  does (recorded). The version is `surrealdb-2.7.0`, `surrealdb-3.3.0`, or a
  string with a build, such as `surrealdb-3.1.6+20260813.cfbaec4`. Both
  principals read it through `surrealdb.Version` on each release (tested, by
  `TestIntegrationVersion`, which step 16 asks for, and D57).
- No statement of SurrealQL returns the version. `RETURN version()` and
  `RETURN surrealdb::version()` are parse errors on both lines (measured with
  curl). `GET /version` returns it as plain text, with no credentials.

## Flavors

- No other product is known to speak this interface.

## Interfaces

The interface table comes from the code, in step 10.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping calls the RPC method ping, which checks the credentials. |
| `driver.SessionResetter` | no | A connection holds no state on the server, so nothing needs a reset. |
| `driver.Validator` | yes | A connection holds no state, so it is always valid. |
| `driver.NamedValueChecker` | yes | An argument must have a name (D50), and it keeps its Go value for the encoding of D53. |
| `driver.QueryerContext` | yes | The server binds each argument itself, through the vars of /rpc (D50). |
| `driver.ExecerContext` | yes | Exec reads every result, and returns the first error (D55). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | no | A transaction of SurrealDB lives in one request, so the driver has none (D54). |
| `driver.RowsColumnScanner` | yes | A value is decoded when it is scanned, and a record id scans into a string as text. |
| `driver.RowsNextResultSet` | yes | Each statement of a request is a result set (D52). |
| `driver.RowsColumnTypeScanType` | no | No type arrives for a column, and each record holds what it holds. |
| `driver.RowsColumnTypeDatabaseTypeName` | no | No type arrives for a column, and each record holds what it holds. |
| `driver.RowsColumnTypeLength` | no | No type arrives for a column, so no column has a length. |
| `driver.RowsColumnTypeNullable` | no | No type arrives for a column, and any field can be NONE or NULL. |
| `driver.RowsColumnTypePrecisionScale` | no | No type arrives for a column, so no column has a precision or a scale. |
<!-- /dbimp:interfaces -->

## Faults

`usql` had no driver for SurrealDB before this one, so there are no faults
to carry over.
The Go SDK shows these faults, which this driver must not repeat:

- It reads the whole body with `io.ReadAll`.
- It panics when it cannot decode an error body.
- `SetTimeout` sets `http.Client.Timeout`, which step 12 of
  [DRIVER.md](DRIVER.md) forbids.

## Second opinions

DeepSeek and Gemini answered the survey of step 5a on 2026-09-27. The
recordings settled where they were wrong:

- Gemini said that `DEFINE SEQUENCE` arrived in 3.0, and DeepSeek said both
  lines have it. It is a parse error on 2.7.0, and works on 3.x (recorded).
- Gemini said that futures were removed in 3.x, which is right: `<future>`
  works on 2.7.0 and is a parse error on 3.x (recorded). DeepSeek said both
  lines have it.
- DeepSeek said that `PARALLEL` and `TEMPFILES` exist on both lines. 3.x
  refuses `PARALLEL` as a parse error, and takes `TEMPFILES` (recorded).
- Gemini said that JSON writes bytes as an array of numbers, which is right.
  DeepSeek said base64, which is wrong (recorded).
- Both said that JSON writes a decimal as a string, which is right
  (recorded).
- DeepSeek said that `/sql` takes a JSON body with `vars`. `/sql` takes plain
  text and binds its query string, which Gemini said (recorded).
- Gemini said that an error in one statement is HTTP 200, which is right.
  DeepSeek said HTTP 400, which is wrong for `/rpc` and right for a parse
  error in `/sql` (recorded).
- Both said that `REFERENCE` exists on 2.x. It is a parse error on 2.7.0
  unless an experimental capability is on, and works on 3.x (recorded).
- Both said that the full text index uses `SEARCH ANALYZER`. That is the
  syntax of 2.7.0, and 3.x takes `FULLTEXT ANALYZER` and refuses `SEARCH`
  (recorded).
- Both said that `LIVE SELECT` exists, which is right, but it is refused over
  HTTP with `ERR`, "Unable to perform the realtime query" (recorded).
- Both said that `VERSION d'...'` exists. The server refused it because
  RocksDB does not keep versions (recorded).

Step 7 asked both models more questions on 2026-09-27, and the server
answered each lead:

- Both said that no statement and no endpoint stops a running query, which
  is right. DeepSeek offered the RPC methods `cancel` and `kill`, the
  statement `SHOW QUERIES` and the endpoints `/queries` and `/running`, and
  none of them does (measured with curl).
- Both said that a transaction cannot span two HTTP requests, which is right.
  DeepSeek offered a header `Surreal-Session`, which did not join them
  (measured with curl).
- Both said that the server neither pages nor streams a result, which is
  right (measured with curl).
- Gemini said that `/rpc` caps a request at 4 MiB, which is right (measured
  with curl).
- Both said that no setting keeps the order of a projection, and that an
  array keeps it, as in `SELECT VALUE [b, a]`. Not measured beyond the order
  of the keys.
- Both said that TLS is served on the port that the server binds. Not
  measured, because `dbrun` starts the server without TLS.

## Open questions

Ken decided the questions of step 9 on 2026-09-27, in D47 to D56 of
[decisions/](decisions/README.md), and how `usql` reads the version in D57.

D70 amends D53: a `RecordID` writes its SurrealQL form as text, through
`MarshalText`, so a record id inside an array prints as `book:earthsea`. The
code holds it, and Ken accepted D70 on 2026-09-29.

Q12 in [PLAN.md](PLAN.md) asked what a result set with no columns gives,
such as the empty array of `DELETE author`. tblfmt D31, in `tblfmt`
`v0.19.1`, answers it, so D52 stands and the driver does not change.
