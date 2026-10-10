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
  requires dbimp `v0.4.0` (`usql/go.mod`, read on 2026-09-29).
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
  statement (recorded). With `Surreal-DB: other`, `session::db()` gives
  `other` for the root user on 2.7.0 and 3.3.0. A user of the database
  `dbmeta` gets `other` on 2.7.0, and HTTP 401 with "There was a problem
  with authentication" on 3.3.0 (measured by hand on 2026-09-29, and
  tested).
- The body of `/rpc` can hold a key that the server does not know, and the
  server ignores it. 3.3.0 sends the `id` of the request back, and 2.7.0
  does not (measured by hand on 2026-09-29).
- The RPC manual names no timeout and no read-only mode for one request.
  The server has `--query-timeout` for every request (the RPC manual, read on
  2026-09-29).
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
- The path can change for one statement, so its two segments are options:
  `WithNamespace` and `WithDatabase` set `Surreal-NS` and `Surreal-DB`
  (D109). The headers of the credentials keep the path of the DSN.
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
- A set is tag 56 around an array on 3.x, and a plain array on 2.7.0. 3.x
  sorts the members of a set, and 2.7.0 keeps their order (recorded).
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

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| none | null | `nil (tag 6)` | `interface {}` | `` | yes |
| null | null | `nil` | `interface {}` | `` | yes |
| bool | boolean | `bool` | `interface {}` | `` | yes |
| int | integer | `int64` | `interface {}` | `` | yes |
| float | float | `float64` | `interface {}` | `` | yes |
| decimal | decimal | `*apd.Decimal (tag 10)` | `interface {}` | `` | yes |
| string | string | `string` | `interface {}` | `` | yes |
| datetime | timestamp | `time.Time in UTC (tags 0 and 12)` | `interface {}` | `` | yes |
| duration | duration | `time.Duration (tags 13 and 14)` | `interface {}` | `` | yes |
| uuid | uuid | `uuid.UUID (tags 9 and 37)` | `interface {}` | `` | yes |
| bytes | binary | `[]byte` | `interface {}` | `` | yes |
| array | array | `[]any` | `interface {}` | `` | yes |
| set | set | `[]any (tag 56)` | `interface {}` | `` | yes |
| object | map | `map[string]any` | `interface {}` | `` | yes |
| record | record id | `RecordID (tag 8)` | `interface {}` | `` | yes |
| geometry | geometry | `map[string]any of GeoJSON (tags 88 to 94)` | `interface {}` | `` | yes |
| range | range | `string, such as 1..5 (tags 49 to 51)` | `interface {}` | `` | yes |
| table | other | `string (tag 7)` | `interface {}` | `` | yes |
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
  exist over HTTP (recorded). `BEGIN` alone is `OK` on 3.3.0, and gives no
  entry on 2.7.0 (recorded). A later request does not join it (measured
  with curl, through the header `Surreal-Session`).
  `BEGIN` with more statements and no `COMMIT` in the same request was
  `ERR`, "Missing COMMIT statement", and a header `Surreal-Session` did not
  join two requests (measured with curl, and the text of that request is
  not kept). A `COMMIT` alone gave the error
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

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true when the server refused the
credential.

- The plain text of HTTP 401, "The password did not verify", matches through
  its `*dbimp.StatusError`. The recordings "a wrong password" (items 76 and
  234) hold it.
- An `Error` with the code 401, which a refusal with a body of JSON gives,
  matches. No recording has that form for a wrong credential, so a test builds
  the error by hand.
- A statement that fails with "IAM error: Not enough permissions" is HTTP 200
  with the status `ERR` and the code 0. It does not match (item 235).
- No other recorded error matches.

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

- The driver answers `SELECT version()` itself, because SurrealQL has no
  statement for the version (D181). It sends the RPC method `version`, the
  request of `surrealdb.Version`, and returns one row with the column
  `version`, a string as the server writes it, such as `surrealdb-3.3.0`.
  Only a query returns the row, and `ExecContext` sends the statement to the
  server. The driver ignores case, the white space around the statement, and
  one final `;`. It takes no argument and no other text. An option such as
  `WithDatabase` has no effect on it (`TestSelectVersion`,
  `TestSelectVersionOnlyThat` and `TestIntegrationVersion`).

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
  curl). `GET /version` returns it as plain text, with no credentials. The
  driver answers `SELECT version()` with the RPC method, and not with `GET
  /version`, because the method already has code and a recording (D181). On
  3.3.0 the administrator and the ordinary user both got `surrealdb-3.3.0`
  from `SELECT version()` on 2026-10-08.

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
| `driver.Pinger` | yes | Ping calls the RPC method ping, with the credentials that every request sends. |
| `driver.SessionResetter` | no | A connection holds no state on the server, so nothing needs a reset. |
| `driver.Validator` | yes | A connection holds no state, so it is always valid. |
| `driver.NamedValueChecker` | yes | An argument must have a name (D50), and it keeps its Go value for the encoding of D53. |
| `driver.QueryerContext` | yes | The server binds each argument itself, through the vars of /rpc (D50). |
| `driver.ExecerContext` | yes | Exec reads every result, and returns the first error (D55). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | BeginTx returns dbimp.ErrNotSupported for every option, because a transaction of SurrealDB lives in one request (D54). |
| `driver.RowsColumnScanner` | yes | A value is decoded when it is scanned, and a record id, a UUID and a duration scan into a string as their text. |
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

Ken decided the questions of the review of D97 on 2026-09-29, in D101,
D109, D110 and D113.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-29 from the staged code that came after `v0.4.0`. A
fact of Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a
fact of SurrealDB from the sections above.

### The server

| | Couchbase | SurrealDB |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /rpc`, with the method `query` and the parameters `[text, vars]` (D50) |
| Database | The key `query_context` of the body | The headers `Surreal-NS` and `Surreal-DB`, from the path of the DSN (D48) |
| Language | SQL++, which is close to SQL | SurrealQL |
| DDL | In SQL++ | In SurrealQL, as `DEFINE` and `REMOVE` |
| Parameters | `?`, `$1` and `$name` | `$name` only. `$1` is a parse error |
| Framing | One body for the whole result, which does not page | One body, with one result for each statement |
| Columns | `signature`, before the first row | No list. Each record has its own keys |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The server sorts the keys of every object |
| Errors | Can come with HTTP 200, after some rows | A parse error and a failed statement both come with HTTP 200. The other statements still run |
| Types | JSON. No date, decimal, UUID or binary | CBOR, with tags for a date, a decimal, a UUID, a duration and a record id, and a byte string |
| Cancel | The server stops a query when the client leaves | The server stops a query when the client leaves. Nothing lists or stops a query |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `BEGIN` and `COMMIT` only inside the text of one request |
| Several statements | Refused | Taken, with one result for each |
| Authentication | Basic, or `creds` in the body | Basic, with `Surreal-Auth-NS` and `Surreal-Auth-DB` for a user of a namespace or a database |
| Default port | 8093, or 18093 with TLS | 8000, with or without TLS |

The differences that a caller sees:

- The columns come from the keys of the first record (D18), and the server
  sorts them, so `SELECT b, a` gives `a, b`. A result whose first value is
  not an object is one column (D101).
- A request can hold several statements, so each one is a result set, and
  `Rows.NextResultSet` moves to the next (D52).
- A transaction cannot span requests, so `BeginTx` fails (D54).
- The values keep their types through CBOR (D49 and D53).

### The driver

| | `couchbase` | `surrealdb` |
| --- | --- | --- |
| Size, without tests, on 2026-09-29 | About 1300 lines in 8 files | About 2900 lines in 12 files, and the CBOR code of the root package |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Namespace`, `Database`, `Auth`, `Encoding` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithNamespace` and `WithDatabase`, through `WithOptions` or an argument (D109). `WithParameter` sets any key of the body of `/rpc`. `WithTimeout` and `WithReadonly` give `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Named only, sent in `vars`. An argument with no name is refused with `dbimp.ErrArguments` |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A concrete reader of the result sets for each format, `cborSets` and `jsonSets`, which walk the answer alike with their own decoders (D108). Each reads the first record ahead to learn the columns |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | None |
| Result sets | One | One for each statement, through `RowsNextResultSet` |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44). A time and a UUID are strings | `int64`, `float64`, `*apd.Decimal`, `time.Time`, `time.Duration`, `uuid.UUID`, `RecordID` and `[]byte`. NONE and NULL are both nil |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported` (D55) |
| Transactions | `BeginTx` sends `BEGIN WORK` | `BeginTx` returns `dbimp.ErrNotSupported` for every option (D54) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | `IsValid` only |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same (D56) |
| Errors | `*ResponseError`, with a list of `Error{Code, Msg}`. A body that is not JSON is a `*dbimp.StatusError`. An error after a row wraps `dbimp.ErrIncomplete` (D107) | `*ResponseError`, with a list of `Error{Code, Kind, Msg}`. A body that is not JSON is a `*dbimp.StatusError` (D55). A failed statement sends no row, so it does not wrap `dbimp.ErrIncomplete` (D107) |
| Authentication | Basic | Basic, with `auth=root`, `auth=namespace` or `auth=database` (D51) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Version` for `Conn.Raw` (D57), `RecordID` (D70), and the constants of `auth` and `encoding` |

The differences that a caller sees:

- A positional argument is refused. A caller must use `sql.Named`, because
  SurrealQL has no positional parameter (D50).
- The columns have no database type, and the scan type of each is `any`,
  because no type arrives for a column (the table of interfaces above).
- `RowsAffected` returns an error, because the server counts no rows (D55).
- Times, UUIDs, durations and record ids arrive as Go types, where Couchbase
  gives strings (D53).
- Only Couchbase takes a `*jsontext.Value` destination, and gives the JSON
  text of an object or an array scanned into a `*[]byte` (D39).
- Ken decided these on 2026-09-29:
  - The driver takes the four options that D109 gives every driver, and
    `WithNamespace` for the first segment of the path. W15 of
    [BACKLOG.md](BACKLOG.md) added them. `WithTimeout` and `WithReadonly`
    give `dbimp.ErrNotSupported`, because a request has neither. The
    `TIMEOUT` clause of SurrealQL limits one statement.
  - `auth` keeps its meaning, the level of the user (D51), as the one
    exception to D94 (D110).
  - A CBOR integer outside the range of `int64` reads as an
    `*apd.Decimal`, and a `uuid.UUID` and a `time.Duration` scan into a
    string as their text of SurrealQL (D113).
