# Apache Druid

This file holds what is known about Apache Druid, for the driver `druid`
(W25, D154 and D162). The headings are the template of
[DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 36.0.0 and 37.0.0 on 2026-10-01 and 2026-10-02, as
`admin`, the administrator, and as `dbmeta_user`, an ordinary user who can
read every datasource. A fact marked "recorded" is in `testdata/druid/`, and
the name in quotes after it is the name of its request in `requests.json`
there. The two releases gave the same answers, except where a line says
otherwise. A request whose name starts with `step 7:` ran on 36.0.0 only. A
fact marked "measured" names how it was measured. Each section starts with
the measured facts. The facts after them come from the sources of step 3,
and each one that is not measured says so. The sources, each read on
2026-10-01, are these:

- "The dbmeta entry" is `container/druid.go` in `dbmeta`. It starts the
  image `docker.io/apache/druid` with ZooKeeper and the five services of the
  nano quickstart in one container, and the basic security extension. The
  administrator is `admin`. Its `Init` makes the user `dbmeta_user` with the
  role `dbmeta_role`, which can `READ` every datasource and nothing else.
- "godruid" is `github.com/IfanTsai/godruid` at commit `64f1a49` of
  2026-09-12, a Go client with a `database/sql` driver in `druidsql/`.
- "kaplanmaxe" is `github.com/kaplanmaxe/go-druid` at commit `1d99314` of
  2020-01-05, a Go `database/sql` driver in `dsql/`.
- "pydruid" is `github.com/druid-io/pydruid` at commit `735ba4a` of
  2026-05-04, the Python client of the Druid project, and its DB-API module
  `pydruid/db/api.py`.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-10-01 and 2026-10-02.

## Summary

- Apache Druid is a column store for analytics. A table is a datasource,
  and each row has the timestamp `__time`. Druid stores a datasource in
  segments, and each segment holds one span of time.
- R holds. `dbrun` starts `druid-36.0.0` and `druid-37.0.0`, in the Staged
  tier with the cadence `tested` (the dbmeta entry, dbmeta D113). Each one
  uses up to 2.7 GB (dbmeta D113). `GET /status` gave the version of each
  (recorded: "the version").
- H and S hold. `POST /druid/v2/sql` on the Router took SQL and answered
  with HTTP 200 as both principals on each release (recorded: "a
  statement").
- The SQL API reads only. `INSERT` and `REPLACE` run as tasks of the
  multi-stage engine through `POST /druid/v2/sql/task`. `UPDATE` and
  `DELETE` fail on both APIs. `REPLACE` of a span of time replaces the rows
  of that span, and a `REPLACE` with no rows removes them (recorded: "crud:
  insert", "crud: update", "crud: replace one day", "crud: delete" and
  "crud: delete one day with replace"). DRIVER.md says to stop and ask Ken
  when a server refuses one of insert, select, update and delete. See the
  open questions.
- The priority is in [TARGETS.md](TARGETS.md): Druid is number 10 of the
  order (D162). D154 gives it a driver of its own, and not a flavor of
  Avatica, although Druid also speaks Avatica at `/druid/v2/sql/avatica/`.
- `dburl` has no scheme for Druid, and `usql` has no driver for it (measured
  with `grep` in `dburl/scheme.go` and `ls usql/drivers` on 2026-10-01).
- `dbmeta` has no model for Druid. Its backlog waits for this driver and a
  scheme in `dburl` (`dbmeta/docs/BACKLOG.md`).
- The driver is `github.com/xo/dbimp/druid` (D164). It reads only (D163).
  Its integration tests passed on `druid-36.0.0` and on `druid-37.0.0` on
  2026-10-02, as both principals (step 14).

## Requests

These facts were recorded on each release:

- A statement is `POST /druid/v2/sql` with `Content-Type:
  application/json` and a JSON object. `query` holds the text. The other
  fields are optional: `resultFormat`, `header`, `typesHeader`,
  `sqlTypesHeader`, `context` and `parameters` (recorded: "a statement").
- A request with no `Content-Type` fails with HTTP 400 and `Missing
  Content-Type header` (recorded: "a statement with no content type"). A
  body of `text/plain` is the SQL text itself, and the answer has the
  default form (recorded: "a statement as plain text"). `GET /druid/v2/sql`
  answers HTTP 405 (recorded: "a GET of the SQL API").
- A field that the server does not know is ignored, in the body and in the
  context (recorded: "an unknown field in the body" and "feature: an unknown
  context key"). An unknown `resultFormat` fails with HTTP 400 (recorded:
  "an unknown result format").
- `context` holds the parameters of one query, such as `sqlQueryId`,
  `sqlTimeZone`, `sqlStringifyArrays`, `sqlOuterLimit` and `timeout`
  (recorded: "feature: a query id", "feature: the time zone in the context",
  "every type with arrays as arrays", "a result with an outer limit" and "a
  timeout in the context"). A `SET` statement before the query sets the
  same keys (recorded: "feature: SET before a statement").
- The answer names the query in `X-Druid-SQL-Query-Id` and
  `X-Druid-Query-Id`. With `sqlQueryId` in the context, both hold that id
  (recorded: "feature: a query id"). `X-Druid-Response-Context` holds a JSON
  object, such as `{"missingSegments":[]}`.
- A write is `POST /druid/v2/sql/task` with the same body. It answers HTTP
  202 with `taskId` and `state` `RUNNING` (recorded: "crud: insert"). `GET
  /druid/indexer/v1/task/<taskId>/status` gives `statusCode` `SUCCESS`
  when the task ends (recorded: "crud: the state of the insert task"). The
  rows of the task answer a query some seconds after it ends: a select 60
  seconds after the task showed them (recorded: "crud: select").
- A statement that the task API refuses before it runs answers HTTP 400
  with `taskId`, `state` `FAILED` and `error` (recorded: "crud: update
  through the task API" and "an error of a task").
- The SQL API refuses `INSERT` with `INSERT operations are not supported by
  requested SQL engine [native], consider using MSQ.` (recorded: "crud:
  insert through the SQL API").
- Authentication is HTTP Basic. A wrong password answers HTTP 401 with a
  page of HTML (recorded: "a wrong password").
- The Router passes `/druid/coordinator/...`, `/druid/indexer/...` and
  `/proxy/coordinator/...` to the Coordinator and the Overlord (recorded:
  "the datasources of the Coordinator", "crud: the state of the insert
  task" and "the users of the security API").

This fact was measured with `curl` on 37.0.0 on 2026-10-01:

- Two tasks of the multi-stage engine that started at the same time never
  ended, and four more waited behind them. Each task needs a slot for its
  controller and one for its worker, and the nano quickstart has two slots.
  `POST /druid/indexer/v1/task/<id>/shutdown` ended them. So the setup of
  `requests.json` waits 40 seconds between two tasks.

## The DSN

- The DSN is `druid://user:password@host:8888`, with the keys `tls`,
  `timezone` and `timeout`, and no path (D164). The port is 8888 when the
  URL names none, with `tls=true` too.
- godruid takes the schemes `druid`,
  `druids`, `http` and `https`, and the keys `header`, `token`, `jwt`,
  `skip_tls_verify`, `ca_cert`, `client_cert`, `client_key`, `proxy`,
  `user` and `password` (godruid, `druidsql/dsn.go`). kaplanmaxe takes the
  address of the Broker, and a path for its ping (kaplanmaxe, `dsn.go`).
- `dbrun` gives each principal as `http://user:password@127.0.0.1:<port>`,
  the address of the Router, which listens on 8888 in the container (the
  dbmeta entry).
- Druid has no database to choose. Every datasource is in the schema
  `druid`, and the other schemas are `sys`, `INFORMATION_SCHEMA` and
  `lookup` (recorded: "schema: the datasources" and "feature: lookup
  table").

## Responses

These facts were recorded on each release:

- The answer is HTTP 200 with `Content-Type: application/json`, for every
  result format, `csv` too (recorded: "a statement" and "the result format
  csv").
- `resultFormat` is `object` by default: one JSON array of objects, each
  with the name of each column (recorded: "a statement with no options").
  `array` is one JSON array of arrays, in the order of the columns.
  `objectLines` and `arrayLines` write one row on each line, and end with
  an empty line. `csv` writes one row on each line, with quotes where they
  are needed, and ends with an empty line (recorded: "the result format
  objectLines", "the result format arrayLines" and "the result format
  csv"). `tsv` is not a format (recorded: "an unknown result format").
- With `header`, the first row holds the names of the columns. With
  `typesHeader` as well, the next row holds the native type of each column,
  such as `LONG` or `ARRAY<STRING>`. With `sqlTypesHeader`, the next row
  holds the SQL type, such as `BIGINT` or `ARRAY` (recorded: "a statement"
  and "only the names of the columns"). In `object` and `objectLines`, the
  first object maps each name to `{"type": ..., "sqlType": ...}` (recorded:
  "the result format object"). `X-Druid-SQL-Header-Included: yes` says
  that the header is there.
- So the names and the types of the columns arrive before the first row,
  in the order of the statement. A result with no rows still has its
  header (recorded: "the columns of an empty result", "the columns of an
  empty result in lines" and "the columns of a limit of zero").
- Two columns with one name keep both in `array`. In `object`, the object
  holds the name twice, as `{"a":1,"a":9223372036854775807}` (recorded:
  "two columns with one name as arrays" and "two columns with one name as
  objects").
- A result of 400 rows came whole in one answer, in the order of `__time`
  (recorded: "a result of 400 rows" and "a result of 400 rows in lines").
  The SQL API does not page. `sqlOuterLimit` cuts the result at that many
  rows, with no sign in the answer, and `LIMIT` with `OFFSET` works
  (recorded: "a result with an outer limit" and "a result with limit and
  offset").
- `ORDER BY` on a column other than `__time`, with no `GROUP BY`, fails
  with `Query could not be planned` (measured with the recorder on 37.0.0
  on 2026-10-01, in a run that a later run replaced).
- On 37.0.0 the datasource `dbimp_t` is in the answers too. A probe with
  `curl` made it before the recordings (recorded: "schema: the
  datasources").
- With `Accept-Encoding: gzip`, the answer has `Content-Encoding: gzip`
  (recorded: "a gzip answer").
- No answer redirects. `GET /druid/coordinator/v1/leader` through the
  Router gives the address of the Coordinator inside the container,
  `http://localhost:8081` (recorded: "a path of the Coordinator through the
  Router").

These facts were recorded as `step 7:` requests on 36.0.0:

- `POST /druid/v2/sql/statements` with `executionMode` `ASYNC` in the
  context runs a query as a task. It answers HTTP 200 with `queryId`,
  `state` `ACCEPTED` and the `schema` of the result (recorded: "step 7: a
  statement of the statements API"). `GET` of the statement gives its state
  and its pages, and `GET .../results?page=0` gives the rows (recorded:
  "step 7: the state of the statement" and "step 7: the first page of the
  statement").
- Without durable storage, the result is one page whatever `rowsPerPage`
  says, and the next page fails with `Page number [1] is out of the range of
  results`. `selectDestination` `durableStorage` fails, because the server
  has no durable storage (recorded: "step 7: the second page of the
  statement" and "step 7: a statement that asks for durable storage").

These facts come from the sources, and are not measured:

- pydruid reads the answer as it arrives, in chunks, and godruid reads the
  rows one at a time (pydruid and godruid). kaplanmaxe reads the whole body
  with `ioutil.ReadAll` (kaplanmaxe, `connection.go`).
- Without durable storage, the statements API keeps at most 3,000 rows
  (Gemini).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table
for Ken to review. The wire type is the SQL type of `sqlTypesHeader`, except
for `OTHER`, which the native type of `typesHeader` names.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| INTEGER | integer | `int64` | `int64` | `INTEGER` | yes |
| FLOAT | float | `float64, from the JSON number of a float32` | `float64` | `FLOAT` | yes |
| REAL | float | `float64` | `float64` | `REAL` | yes |
| DOUBLE | float | `float64, with the infinities and NaN from their strings` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `float64, because the server computes it as a double (D164)` | `float64` | `DECIMAL` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| VARCHAR | string | `string. A multi-value string with more than one value is a []any of its strings (D164)` | `string` | `VARCHAR` | yes |
| CHAR | string | `string` | `string` | `CHAR` | yes |
| TIMESTAMP | timestamp | `time.Time, from the ISO 8601 text with the offset of sqlTimeZone` | `time.Time` | `TIMESTAMP` | yes |
| DATE | date | `dbimp.Date, from the date of the ISO 8601 text of its midnight in sqlTimeZone` | `dbimp.Date` | `DATE` | yes |
| ARRAY | array | `[]any, of the Go types of the elements that the native type names, from the JSON text or the JSON array` | `[]interface {}` | `ARRAY` | yes |
| COMPLEX<json> | json | `the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any, from the JSON text in a string` | `interface {}` | `OTHER` | yes |
| COMPLEX<HLLSketch> | binary | `[]byte, from the base64 in the JSON text of a string` | `[]uint8` | `OTHER` | yes |
| NULL | null | `nil` | `interface {}` | `NULL` | yes |
<!-- /dbimp:types -->

These facts were recorded on each release, from the datasource
`dbimp_types` with three rows: one with values, one with empty or small
values, and one of NULL (recorded: "every type", unless a line says
otherwise):

- A NULL is the JSON `null` in every type and every JSON format. In `csv`,
  a NULL and an empty string are both an empty field (recorded: "every type
  as csv").
- A `BIGINT` is a JSON number, and keeps every digit from
  -9223372036854775808 to 9223372036854775807. A literal outside that range
  fails with `Numeric literal ... out of range` (recorded: "an integer
  literal out of range").
- `CAST(... AS INTEGER)` gives `INTEGER` with the native type `LONG`.
  `CAST(... AS SMALLINT)` and `CAST(... AS TINYINT)` fail with `Cannot coerce
  field [id] from type [java.lang.Integer] to type [SMALLINT]` (recorded:
  "casts to other types", "a smallint" and "a tinyint").
- A `FLOAT` column holds a float32: `3.4e38` reads back as `3.4E38`, and as
  `3.3999999521443642E38` when cast to `DOUBLE` (recorded: "a float near
  its limit"). `REAL` is a `DOUBLE` in the native type.
- A `DOUBLE` is a JSON number, and an infinity or NaN is the string
  `"Infinity"`, `"-Infinity"` or `"NaN"` (recorded: "an infinity and NaN").
  `CAST('NaN' AS DOUBLE)` fails (measured with `curl` on 37.0.0 on
  2026-10-01).
- A `DECIMAL` has the native type `DOUBLE`. `CAST(d AS DECIMAL(38, 10))` of
  `1.7976931348623157e308` reads `1.7976931348623157E308`, and the literal
  `1234567890.123` reads `1.234567890123E9` (recorded: "casts to other
  types", and measured with `curl` on 37.0.0 on 2026-10-01).
- A `BOOLEAN` of the query, such as `id = 1`, is a JSON boolean with the
  native type `LONG`. A boolean written to a datasource is stored as a
  `BIGINT` of 1 or 0 (recorded: "a boolean column of the query" and "every
  type").
- A `VARCHAR` is a JSON string, with every character, and an empty string
  stays empty (recorded). A literal string is `CHAR` (recorded: "literals of
  each type").
- A multi-value string has the SQL type `VARCHAR` and the native type
  `STRING`. One value is that string, and two values are the JSON text of an
  array of strings, `"[\"x\",\"y\"]"`, whatever `sqlStringifyArrays` says.
  `GROUP BY` gives one row for each value (recorded: "feature: multi-value
  strings" and "feature: a multi-value string grouped"). `MV_TO_ARRAY`
  gives a JSON array of strings (recorded: "step 7: a multi-value string as
  an array").
- A `TIMESTAMP` is ISO 8601 text with milliseconds, such as
  `"2026-10-01T12:34:56.789Z"`. With `sqlTimeZone` `Asia/Jakarta`, it is
  `"2026-10-01T19:34:56.789+07:00"` (recorded: "feature: the time zone in
  the context"). `0001-01-01T00:00:00.000Z` and `1900-01-01T00:00:00.000Z`
  read back as written (recorded: "a timestamp before 1970").
- A `DATE` is the ISO 8601 text of its midnight, such as
  `"2026-10-01T00:00:00.000Z"`, or `"2026-10-01T00:00:00.000+07:00"` with
  `sqlTimeZone` (recorded: "casts to other types" and "feature: the time zone
  in the context").
- An `ARRAY` has the native type `ARRAY<STRING>`, `ARRAY<LONG>` or
  `ARRAY<DOUBLE>`. By default it is the JSON text of the array, such as
  `"[\"a\",null,\"c\"]"`. With `sqlStringifyArrays` false, it is a JSON
  array (recorded: "every type" and "every type with arrays as arrays"). An
  element can be NULL. `ARRAY[]` fails with `Require at least 1 argument`
  (measured with `curl` on 37.0.0 on 2026-10-01).
- A JSON column has the SQL type `OTHER` and the native type
  `COMPLEX<json>`. It is the JSON text in a string, such as
  `"{\"k\":[1,\"two\",null,{\"n\":1.5}]}"`, with `sqlStringifyArrays` true or
  false (recorded: "every type" and "every type with arrays as arrays").
- An HLL sketch has the SQL type `OTHER` and the native type
  `COMPLEX<HLLSketch>`. It is the JSON text of a string of base64, such as
  `"\"AgEHDAMIAQD2fr0F\""` (recorded: "feature: a sketch").
- A NULL literal has the SQL type `NULL` and the native type `STRING`
  (recorded: "literals of each type" and "typed parameters").
- `TIME` fails with `Unsupported DateTime type[TIME]`, and `VARBINARY`
  fails with `Unhandled Query Planning Failure` (recorded: "a time of day"
  and "a binary value").

These facts come from the sources, and are not measured:

- godruid gives a `TIMESTAMP` and a `DATE` as a `string`, and an array or a
  JSON value as its JSON text in a `[]byte` (godruid, `druidsql/rows.go`).
- Other sketches, such as a theta sketch, arrive as base64 too (Gemini).

## Parameters

These facts were recorded on each release:

- A statement names each parameter as `?`. `parameters` is a list of
  `{"type": ..., "value": ...}`, in the order of the marks. A type is a SQL
  type, such as `VARCHAR`, `BIGINT`, `DOUBLE`, `BOOLEAN`, `TIMESTAMP`,
  `DATE`, `DECIMAL` or `ARRAY` (recorded: "typed parameters" and "an array
  parameter").
- A `TIMESTAMP` takes its text, such as `2026-10-01 12:34:56.789`, or its
  milliseconds since 1970-01-01 (recorded: "a timestamp parameter in
  milliseconds"). A `DATE` takes its text (recorded: "a date parameter").
- A `VARCHAR` of `null` binds NULL (recorded: "typed parameters").
- Too few values fail with `No value bound for parameter (position [2])`.
  Too many are ignored (recorded: "too few parameters" and "too many
  parameters").
- The `INTEGER` `"1"`, a string, fails with HTTP 500 and `Cannot handle
  query`. A parameter with no `type` fails with HTTP 400 and
  `NullPointerException` (recorded: "a string for an integer" and "a
  parameter with no type").
- A `DECIMAL` binds as the type of its JSON number: the value
  `1.2345678901234568e18` came back as the `BIGINT` 1234567890123456768
  (recorded: "a decimal parameter").
- `:id` is a syntax error. A `?` inside a quoted string is text, and not a
  parameter (recorded: "a named parameter" and "a parameter in a quoted
  string").
- A `?` whose type the statement cannot infer fails with `Illegal use of
  dynamic parameter` (measured with `curl` on 37.0.0 on 2026-10-01).

These facts come from the sources, and are not measured:

- godruid sends each argument as a parameter, with `VARCHAR` for nil and
  for `[]byte`, `BIGINT` for an integer, `DOUBLE` for a float and
  `TIMESTAMP` in milliseconds for a `time.Time`. It refuses a named
  argument (godruid, `sqlparam.go` and `druidsql/conn.go`).
- pydruid writes each argument into the text as a literal, with `'` doubled
  in a string (pydruid). kaplanmaxe ignores the arguments (kaplanmaxe).

## Transactions

- Druid has no transactions. `BEGIN` and `COMMIT` fail with a syntax error
  (recorded: "begin a transaction" and "commit a transaction").
- A task replaces whole segments, so a write of one span of time is
  atomic (Gemini, not measured).

## Errors

These facts were recorded on each release:

- An error before any rows answers with a JSON object: `error`,
  `errorCode`, `persona`, `category`, `errorMessage` and `context`. A
  syntax error and an unknown datasource are HTTP 400 with `category`
  `INVALID_INPUT` (recorded: "a syntax error" and "an unknown
  datasource").
- An error while the query runs, such as a division of a `BIGINT` by zero,
  is HTTP 500 with `errorClass` `java.lang.ArithmeticException` and
  `category` `RUNTIME_FAILURE`. Its `errorMessage` is `/ by zero` on 36.0.0
  and `null` on 37.0.0 (recorded: "an error before any rows"). A
  query that the server cannot plan is HTTP 400 or HTTP 501 (recorded: "a
  binary value").
- A query that passes its `timeout` is HTTP 504 with `category` `TIMEOUT`
  (recorded: "a timeout in the context").
- An error after some rows ends a response of HTTP 200 with no sign of the
  error. The query divided by zero in the last row, which is in a second
  segment. In `array`, the body ends after row 397 with no closing `]`. In
  `arrayLines` and `objectLines`, it ends with no empty line. Rows 398 and
  399 never arrived, and the connection ended in the normal way (recorded:
  "an error after some rows", "an error after some rows in lines" and "an
  error after some rows as objects").
- A wrong password is HTTP 401 with a page of HTML. A refused privilege is
  HTTP 403 with `{"Access-Check-Result": ...}` (recorded: "a wrong
  password" and "a system table that needs a privilege").
- No request answered HTTP 429.

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose
`HTTPStatus` is 401. That is the answer to "a wrong password", a page of
HTML. It is also true through the `*dbimp.StatusError` that the `*Error`
wraps.

It is false for HTTP 403, which Druid sends for a missing privilege: "a
refused privilege" (`Access-Check-Result`) and "a refused task" (the category
`FORBIDDEN`). It is false for every other status.

## Cancellation and timeouts

These facts were recorded on each release:

- `DELETE /druid/v2/sql/<sqlQueryId>` cancels a running query, and answers
  HTTP 202. The query then answers HTTP 500 with `Sequence canceled`. An id
  that is not running answers HTTP 404 (recorded: "a statement to cancel",
  "cancel the statement by its id" and "cancel a statement that is not
  running"). The ordinary user cancelled its own query.
- `timeout` in the context, in milliseconds, stops the query on the server
  (recorded: "a timeout in the context").
- A statement after a client left answered at once (recorded: "a statement
  after the client left").

These facts were measured with `curl` and the processor time of the
container in `/sys/fs/cgroup/cpu.stat`, on 37.0.0 on 2026-10-01:

- A query that the client left after 2 seconds used about 2.3 seconds of
  processor time in each 2 seconds until its `timeout`, 8 seconds later. So
  the server does not stop a query when the client leaves.
- After `DELETE /druid/v2/sql/<sqlQueryId>`, the processor time dropped to
  the idle level at once.
- As `dbmeta_user`, `DELETE` of the query of `admin` answered HTTP 404.

These facts come from the sources, and are not measured:

- The server notices a client that left only when it writes to the socket
  (Gemini).
- godruid cancels a query of the statements API with `DELETE
  /druid/v2/sql/statements/<id>` (godruid, `statement.go`).

## Statements

These facts were recorded on each release:

- Two statements in one request fail: `Only SET statements can appear
  before the final statement in a statement list`. `SET` statements before
  the last one set keys of the context (recorded: "two statements in one
  request" and "feature: SET before a statement").
- Comments `--` and `/* */` are part of the text, and a statement can end
  with a semicolon (recorded: "comments" and "a statement that ends with a
  semicolon").
- `EXPLAIN PLAN FOR` gives the native query as JSON text in one row
  (recorded: "feature: explain").
- `UPSERT` fails with `UPSERT is not supported.` on the task API (recorded:
  "step 7: upsert through the task API").
- `CREATE TABLE`, `CREATE INDEX`, `CREATE VIEW`, `ALTER TABLE` and `DROP
  TABLE` fail with a syntax error, which names `INSERT`, `UPSERT`,
  `EXPLAIN`, `SET` and `RESET` among the statements that it expects
  (recorded: "schema: create table", "schema: create index", "schema:
  create view", "schema: alter table" and "schema: drop table"). A
  datasource is made by its first write, and `DELETE
  /druid/coordinator/v1/datasources/<name>` drops it (recorded: "teardown:
  drop the datasource for crud").

These facts were measured by the integration tests of the driver, on 36.0.0
and 37.0.0 on 2026-10-02:

- A task of the multi-stage engine took from 5 to 12 seconds, and its rows
  answered a query some seconds after it ended. A dropped datasource left
  the answers in about one second (`TestIntegrationSchema`).
- `POST /druid/coordinator/v1/datasources/<name>/markUnused` with an
  interval drops the rows of that interval from the answers, with no task
  (`TestIntegrationRoundTrip`).
- `CLUSTERED BY id` writes a segment of the shard spec `numbered`, whose
  rows are in the order of `__time` and then of `id`, and a query with no
  `ORDER BY` reads them in that order (`TestIntegrationSchema`).
- A segment whose JSON column holds only NULL stores the column as a
  `VARCHAR`. A datasource whose only segment is such a segment answers the
  column as a `VARCHAR` (`TestIntegrationRoundTrip`).
- `ORDER BY` on a column other than `__time`, with no `GROUP BY`, fails with
  `Query could not be planned`, on a lookup table too
  (`TestIntegrationFeatures`).
- A lookup that the Coordinator makes answers `LOOKUP` within about 60
  seconds (`TestIntegrationFeatures`).
- The task API refuses `CAST(NULL AS VARCHAR ARRAY)` with `Cannot handle
  literal [null:VARCHAR NOT NULL ARRAY] of unsupported type [ARRAY]`. A
  NULL array that `EXTERN` reads from a row of JSON is written
  (`TestIntegrationRoundTrip`, on 36.0.0).
- When the host was under heavy load, the Router answered HTTP 503 with
  `Unable to determine destination` for the state of a task, and the task
  API refused a task with HTTP 500 that names HTTP 503 of the Overlord. The
  tests read the state again, and submit such a task again (on 36.0.0).

## Principals

These facts were recorded on each release:

- `dbmeta_user` reads every datasource, `INFORMATION_SCHEMA`,
  `sys.segments`, `sys.tasks` and the lookups (recorded as `dbmeta_user`).
- `dbmeta_user` is refused `sys.servers`, `GET /status`, the security API,
  the leader of the Coordinator, and every task, with HTTP 403 (recorded as
  `dbmeta_user`: "a system table that needs a privilege", "the version",
  "the users of the security API", "a path of the Coordinator through the
  Router" and "crud: insert").
- So `dbmeta_user` cannot read the version. `admin` reads it from `GET
  /status` and from `SELECT server_type, version FROM sys.servers`
  (recorded: "the version" and "the version of each service").
- `dbmeta_user` reads `GET /druid/coordinator/v1/datasources` (recorded:
  "the datasources of the Coordinator").
- The driver does not answer `SELECT version()` itself (D181). Druid has a
  query for the release, `SELECT server_type, version FROM sys.servers`, and
  D181 gives no support to a product that has one. On `druid-38.0.0` on
  2026-10-08, `SELECT version()` and `SELECT DRUID_VERSION()` failed with
  `No match found for function signature`, and `sys.servers` gave `38.0.0` to
  `admin` and HTTP 403 to `dbmeta_user`. `GET /status` gave `38.0.0` to
  `admin` and HTTP 403 to `dbmeta_user`. The driver sends every statement to
  the server, so the ordinary user gets the error of the server.

Step 16 ran the statement for the version through the driver, on 36.0.0 and
37.0.0 on 2026-10-02 (`TestIntegrationVersion`). `usql` has no driver for
Druid, so the statement is `SELECT server_type, version FROM sys.servers`,
which answers for each service:

- `admin` reads one row for each service, `broker`, `coordinator`,
  `historical`, `middle_manager`, `overlord` and `router`, each with the
  version of the release, such as `36.0.0`.
- `dbmeta_user` gets HTTP 403 with `Insufficient permission to view
  servers: Unauthorized`. So the ordinary user gets no version.

## Flavors

- No flavor was measured.
- Imply Enterprise and Imply Polaris serve the same SQL API, and Polaris
  adds its own authentication (Gemini, not measured).

## Interfaces

`druid/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials. GET /status needs more than the privilege to read. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because the SQL API has no sessions. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a decimal, a dbimp.Date, a dbimp.LocalDateTime and a slice, which the driver binds with a type of their own (D164). |
| `driver.QueryerContext` | yes | The server binds each argument as a typed parameter (D164). |
| `driver.ExecerContext` | yes | Exec reads the result to its end. The SQL API takes no write, so RowsAffected fails (D163). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because Druid has no transactions (D164). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, after any SET, so an answer has one result. |
| `driver.RowsColumnTypeScanType` | yes | The header names the SQL type and the native type of each column (D164). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The header names the SQL type of each column, such as BIGINT, or OTHER for a complex type. |
| `driver.RowsColumnTypeLength` | no | The header names no length. |
| `driver.RowsColumnTypeNullable` | yes | The header does not say whether a column can be NULL, and every type can be. |
| `driver.RowsColumnTypePrecisionScale` | no | The header names no precision and no scale, and a DECIMAL is a double. |
<!-- /dbimp:interfaces -->

## Faults

These are faults of the Go drivers that a driver here does not repeat:

- kaplanmaxe reads the whole body with `ioutil.ReadAll`, returns
  `driver.ErrSkip` from `Prepare` and `Begin`, ignores the arguments, and
  creates an `http.Transport` for each query (kaplanmaxe, `connection.go`).
- godruid gives an array and a JSON value as the JSON text in a `[]byte`,
  and a `TIMESTAMP` as a `string` (godruid, `druidsql/rows.go`).
- godruid answers `Exec` with `RowsAffected(0)`, and sends a write to the
  SQL API, which refuses it (godruid, `druidsql/conn.go`).
- Neither Go driver notices an error after some rows, because neither one
  checks for the closing `]` or the empty line (godruid and kaplanmaxe).

## Second opinions

Step 7 tested each lead on the server. Gemini was asked on 2026-10-02.
DeepSeek timed out on each of the four times that it was asked the
questions of step 7, on 2026-10-02.

- Named parameters. Gemini said that Druid has only `?`. The server agrees
  (recorded: "a named parameter").
- Transactions. Gemini said that there are none, and that a task replaces
  segments as one change. The server refuses `BEGIN`.
- Paging. Gemini said that the statements API pages, but that without
  durable storage the result is one page of at most 3,000 rows. The server
  gave one page, and refused durable storage.
- Types. Gemini said that a `DATE` can be text with no time. The server
  sends the text of its midnight. Gemini said that `MV_TO_ARRAY` with
  `sqlStringifyArrays` false gives a typed array. The server agrees.
- Cancel. Gemini said that the server does not stop a query when the
  client leaves. The server agrees.
- `UPSERT`. Gemini said that it is a word of the grammar of Calcite, and
  that Druid refuses it. The server agrees.
- The end of a result. Gemini said that `array` and `object` end with `]`,
  and that the lines formats have no sign of their end. The server ends
  each lines format with an empty line, and leaves it out after an error.

Step 8a asked both models on 2026-10-02 to review the mapping of the
types against [TYPES.md](TYPES.md) and D135. DeepSeek timed out on each of
three tries. Gemini answered:

- It agreed with the integers, the floats with their strings, the boolean,
  the strings, the timestamp, the date, the array, the JSON value and the
  sketch as bytes.
- It said that a `DECIMAL` is a `float64`, because its native type is
  `DOUBLE` and it holds no exact digits. The table follows D135, which gives
  a decimal an `*apd.Decimal`. See the open questions.
- It said that a multi-value string stays a `string`, because the server
  names it `VARCHAR`.
- It said that the SQL type `NULL` needs a row of the kind null, for a
  `SELECT NULL`. The table has one.
- It said that a boolean stored in a datasource is a `BIGINT`, because the
  server names it so. The server does (recorded: "every type").

The coordinator asked DeepSeek again on 2026-10-02, through
`deepseek-v4-pro`, because `deepseek-flash` timed out once more. It
answered:

- It agreed with the integers, the floats with their strings, the boolean,
  the timestamp, the date, the array, the JSON value, the sketch as bytes
  and NULL.
- It said that a `DECIMAL` is a `float64`, because the server computes it as
  a double, and an `*apd.Decimal` would claim a precision that the server
  does not keep. Gemini said the same. See the open questions.
- It said that a multi-value string with more than one value is a `[]any`,
  decoded from the JSON text, and a single value stays a `string`. Gemini
  said the opposite. See the open questions.

## Open questions

Ken settled the first four questions of step 8 on 2026-10-02, and D163 and
D164 hold the answers:

1. The driver reads only. It sends `INSERT` and `REPLACE` to the SQL API,
   which refuses them, and never to the task API (D163).
2. The driver reads `arrayLines`, and an answer that ends with no empty line
   is an error that wraps `dbimp.ErrIncomplete` (D164).
3. A `DECIMAL` is a `float64` (D164).
4. A multi-value `VARCHAR` with more than one value is a `[]any` of its
   strings, and one value stays a `string` (D164).

Two facts wait for `dbmeta`, and not for the driver (D164): `dbmeta_user`
cannot read the version, and the nano quickstart has two task slots, so the
tests run one task at a time.

Ken answered the questions of the steps after step 9 on 2026-10-07, and D164
holds the answers:

1. The driver reads a plain string whose text is a JSON array of two or more
   strings, such as `["x","y"]`, as a `[]any` too, because the server names
   it as it names a multi-value string (`druid/types.go`).
2. The driver cancels a query by its id when the context of the query ends
   before the driver reads the whole answer, and when the caller closes the
   rows before their end, as Neo4j does (D105).
3. The port is 8888 when the DSN names none, with `tls=true` too, as Pinot
   keeps 8099 (D129).
4. `dbrun` gives the `url` of each principal as `http://`, and the tests read
   it as the DSN `druid://` (`druid/integration_test.go`). Ken asked the
   `dbmeta` session to give the six new entries the scheme of their driver,
   as D156 did for Avatica. Until it does, the conversion stays in the
   tests.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-10-02 from the staged code. A fact of Couchbase comes
from [COUCHBASE.md](COUCHBASE.md), and a fact of Druid from the sections
above.

### The server

| | Couchbase | Apache Druid |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /druid/v2/sql` on the Router, with `query`, `resultFormat`, the three keys of the header, `context` and `parameters` (Requests) |
| Database | The key `query_context` of the body | None. Every datasource is in the schema `druid` (The DSN) |
| Language | SQL++, which is close to SQL | SQL, parsed by Calcite. A write is SQL of the multi-stage engine, which only the task API runs (Requests) |
| DDL | In SQL++ | None. A write makes a datasource, and the Coordinator drops it. Every statement of DDL is a syntax error (Statements) |
| Parameters | `?`, `$1` and `$name` | `?` only, each a typed `{type, value}` (Parameters) |
| Framing | One body for the whole result, which does not page | One body for the whole result, which the server writes as the query runs. `arrayLines` writes one row on each line, and an empty line at the end (Responses) |
| Columns | `signature`, before the first row | The three rows of the header: the names, the native types and the SQL types (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | Before any row, a status that is not 2xx with a JSON object. After some rows, HTTP 200 with no empty line at the end and no other sign (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a SQL type and a native type for each column. A time is ISO 8601 text, an array is JSON text, a JSON value is its text, and a sketch is base64 (Types) |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves, and `DELETE /druid/v2/sql/<sqlQueryId>` stops it, which the driver sends when the context ends or the rows close early (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` is a syntax error (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic |
| Default port | 8093, or 18093 with TLS | 8888 on the Router |

The differences that a caller sees:

- Every write fails with the refusal of the server, because the SQL API
  takes none (D163).
- A query has no database, and `WithDatabase` fails (D164).
- The server binds each argument as a typed parameter that the driver names
  from the Go type of the argument, and a named argument is refused (D164).
- A result that the server cut short after some rows fails with
  `dbimp.ErrIncomplete` and `druid.ErrCut`, because the answer ends with no
  empty line (D164).
- The server does not stop a query when the client leaves, so the driver
  cancels it by its id when its context ends (D164).

### The driver

| | `couchbase` | `druid` |
| --- | --- | --- |
| Size, without tests, on 2026-10-02 | About 1300 lines in 8 files | About 1500 lines in 8 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `TimeZone`, `Timeout` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithTimeZone`, through `WithOptions` or an argument (D109). `WithTimeout` sends `timeout` in the context. `WithReadonly` changes nothing, because every statement is read-only. `WithDatabase` gives `dbimp.ErrNotSupported`. `WithParameter("context")` replaces the whole context, the id of the query too |
| Arguments | Sent to the server as `args` and `$name` | Sent as `parameters`, each with the SQL type of its Go type: `BIGINT`, `DOUBLE`, `BOOLEAN`, `VARCHAR`, `TIMESTAMP` in milliseconds, `DATE`, `DECIMAL` and `ARRAY`. A named argument, a `[]byte`, `NaN` and an infinity are refused (`druid/types.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A reader of its own for `arrayLines`, which keeps the last two bytes of the body to see the empty line (`druid/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, the SQL type, and `ColumnTypeScanType`, from the SQL type or the native type of `OTHER`. Every column can be NULL |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column: `int64`, `float64` for a `DECIMAL` too, `bool`, `string`, `[]any` for a multi-value string with more than one value, `time.Time`, `dbimp.Date`, `[]any` for an array, the decoded value of JSON, and `[]byte` for a sketch (D164) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, because the SQL API changes no rows (D163) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D164) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | Each query has its own `sqlQueryId`. When the context ends before the driver read the whole answer, the driver sends `DELETE /druid/v2/sql/<sqlQueryId>` with a limit of 5 seconds (D164) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Category, Persona, Class, Message}`, which unwraps to `*dbimp.StatusError`, and `ErrCut` for an answer with no empty line |
| Authentication | Basic | Basic, and the driver follows no redirect, so the credentials go to the host of the DSN only (D164) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error` and `ErrCut` |

The differences that a caller sees:

- A value keeps its type, a time and a sketch too, where Couchbase gives
  JSON shapes (D135 and D164).
- A `DECIMAL` is a `float64`, where D135 gives a decimal an `*apd.Decimal`,
  because the server computes it as a double (D164).
- A multi-value string with more than one value is a `[]any`, so one column
  can give a `string` in one row and a `[]any` in the next (D164).
- `WithReadonly` succeeds and does nothing, where Couchbase sends
  `readonly`, because the SQL API takes no write (D163).
- `RowsAffected` always returns an error, where Couchbase counts every
  statement by `mutationCount` (D163).
- A result that the server cut short fails with `ErrCut` and
  `dbimp.ErrIncomplete` after its rows (D164).
