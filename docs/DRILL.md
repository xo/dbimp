# Apache Drill

This document holds what is known about Apache Drill, for its driver (W26 and
D162). Steps 3 to 8a of [DRIVER.md](DRIVER.md) filled it. The headings are
the template of [DRIVER.md](DRIVER.md).

Steps 5a to 7 measured `drill-1.21.2` and `drill-1.22.0`, as `admin` and as
`dbmeta_user`, from 2026-10-01 to 2026-10-02. A fact marked "recorded" is in
`testdata/drill/`, and the name in quotes after it is the name of its request
in `requests.json` there. A fact marked "measured" names how it was
measured. The two releases gave the same answers, except where a line says
otherwise. Each section starts with the measured facts. The facts after them
come from the sources below, and each one that is not measured says so.

The sources, each read on 2026-10-01, are these:

- "The REST source" is `exec/java-exec/src/main/java/org/apache/drill/exec/server/rest/`
  in `github.com/apache/drill` at the tag `drill-1.22.0`: `QueryResources.java`,
  `QueryWrapper.java`, `BaseQueryRunner.java`, `stream/QueryRunner.java`,
  `stream/StreamingHttpConnection.java` and `BaseWebUserConnection.java`.
- "The Python driver" is `github.com/JohnOmernik/sqlalchemy-drill`, at its
  branch `master` of 2026-09-16. Its module `drilldbapi` speaks the REST
  interface.
- "The Go driver" is `github.com/factset/go-drill`, at its branch `master` of
  2024-03-13. It speaks the native protocol of Drill on the user port 31010,
  and not the REST interface.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-10-01 and 2026-10-02. One review of step 8a went to
  `deepseek-v4-pro`, as Second opinions says.
- "The dbmeta entry" is `container/drill.go` in `dbmeta`, with dbmeta D118.

## Summary

- Apache Drill is a SQL engine that queries files and other stores in
  place, with no schema that a person declares first. It is under the Apache
  2.0 licence (the dbmeta entry).
- R holds. `dbrun` starts `drill-1.21.2` and `drill-1.22.0`, each in the
  Staged tier with the cadence `tested` (the dbmeta entry, and measured with
  `dbrun dsn --json` on 2026-10-01). The image runs Drill in embedded mode,
  one process with no ZooKeeper.
- H holds. `POST /query.json` on port 8047 answers SQL as JSON, on both
  releases, as both principals (recorded: "a statement").
- S holds. The language is SQL, parsed by Apache Calcite (recorded: "feature:
  explain").
- The priority is P1. D162 names Drill second of the six targets after
  Avatica.
- `dburl` has no scheme for Drill (measured with `grep -n -i drill
  ../dburl/scheme.go` on 2026-10-01, which found nothing). `usql` has no
  driver for Drill (measured with `ls ../usql/drivers` on 2026-10-01). So
  this driver replaces nothing.
- `dbmeta` has no model for Drill. Its entry turns on the `htpasswd`
  authenticator with the users `admin` and `dbmeta_user`, and asks for HTTP
  basic authentication. `admin` is the one administrator, and `dbmeta_user`
  can query and cannot change an option or a storage plugin.
- The server can query these storage plugins with no data loaded: `cp`, the
  sample files on the classpath, such as `employee.json` (1155 rows) and
  `tpch/nation.parquet`, `sys`, `information_schema`, and `dfs`, whose
  workspace `dfs.tmp` is the one that can be written (recorded: "feature:
  information_schema", which names `IS_MUTABLE` `YES` for `dfs.tmp` only).

## Requests

These facts were recorded on each release, as both principals:

- A statement is `POST /query.json` with `Content-Type: application/json`
  and a JSON object (recorded: "a statement"). The object has seven keys
  that the server knows: `query`, `queryType`, `autoLimit`, `userName`,
  `defaultSchema`, `options` and `autoLimitRowCount` (recorded: "an unknown
  key", whose error lists them).
- `queryType` is needed, and `SQL` runs SQL in any case, such as `sql`
  (recorded: "the query type in lower case"). A body without it fails with
  HTTP 400 and the text of a Java error (recorded: "no query type"). The
  text differs between the releases. `PHYSICAL` with SQL in `query` fails
  with no message (recorded: "a physical plan as the query type").
- A key that the server does not know fails with HTTP 400 and
  `Unrecognized field` in plain text (recorded: "an unknown key"). A body
  that is not JSON fails with HTTP 400 in plain text (recorded: "a body that
  is not JSON"). A body sent as `text/plain` fails with HTTP 500 and
  `{"errorMessage" : "HTTP 415 Unsupported Media Type"}` (recorded: "a body
  of plain text").
- `defaultSchema` names the schema of a table whose name has none, such as
  `dfs.tmp` (recorded: "the default schema dfs.tmp"). A schema that does not
  exist fails with HTTP 500 and `{"errorMessage" : "Query submission
  failed"}`, with no reason (recorded: "an unknown default schema").
- `options` sets session options for the one request, each as a string,
  such as `{"exec.query.max_rows": "7"}` (recorded: "the option
  exec.query.max_rows of 7"). An option that does not exist fails with
  HTTP 500 and `Query submission failed` (recorded: "an unknown option").
- `userName` asks to run as another user. Impersonation is off in the
  image, so it fails with HTTP 500 and `Query submission failed` (recorded:
  "a user to impersonate").
- Each request carries HTTP basic authentication. A wrong password gets
  HTTP 307 to `/mainLogin?redirect=%2Fquery.json` with an empty body, and
  not HTTP 401 (recorded: "a wrong password").
- The form login, `POST /j_security_check`, answers HTTP 500 with
  `HTTP 404 Not Found`, because the image turns on basic authentication only
  (recorded: "a form login").
- Each answer sets the cookie `Drill-Session-Id` (recorded, and the recorder
  writes its value as `REDACTED`). A request with no cookie starts a new
  session. So `ALTER SESSION` in one request does not reach the next
  (recorded: "feature: alter session" and "feature: a session option in the
  next request"), and a temporary table is gone in the next request
  (recorded: "schema: temporary table read in the next request").
- A request with `Accept-Encoding: gzip` gets no gzip (recorded: "a gzip
  answer").
- `GET /status.json` gives `{"status" : "Running!"}`, and `GET
  /cluster.json` gives the version of each Drillbit (recorded: "the status"
  and "the cluster").

These facts were measured with `curl`:

- A request with no credentials gets HTTP 404 and a page of HTML (measured on
  2026-10-01 and 2026-10-02, as the dbmeta entry says).
- A request that sends the cookie back with basic authentication keeps its
  session. `ALTER SESSION SET exec.query.max_rows = 2` in one request cut
  the next request to 2 rows (measured on 2026-10-01 and 2026-10-02).
- A session that is idle ends after 3600 seconds, by the boot option
  `drill.exec.http.session_max_idle_secs` (measured from `sys.boot` on
  2026-10-02).

These facts come from the sources, and are not measured:

- The authentication of the HTTP interface is a form with a session cookie,
  SPNEGO, or HTTP basic, by the boot option `drill.exec.http.auth.mechanisms`
  (the REST source). The Python driver logs in with the form and keeps the
  cookie.
- Before Drill 1.19, the server built the whole result in memory before it
  sent it. From 1.19, it streams the result, and `metadata` comes before the
  rows (a comment in `QueryResources.java`).

## The DSN

- `dburl` has no scheme for Drill, so step 9 decides the URL (D27 and D35).
- The dbmeta entry gives each principal as `http://user:password@host:port`,
  with the port 8047.
- These settings of a request can come from the DSN: `defaultSchema`,
  `autoLimit`, and session options in `options` (recorded, under Requests).
  None is decided yet.

## Responses

These facts were recorded on each release, as both principals:

- A result is one JSON object, with its members in this order: `queryId`,
  `columns`, `metadata`, `attemptedAutoLimit`, `rows` and `queryState`. Each
  member and each row starts a new line (recorded: "a statement"). A large
  result comes with `Transfer-Encoding: chunked` (recorded: "a result of
  1155 rows").
- `columns` names the columns in the order of the statement, and `metadata`
  names the type of each, in the same order, before the first row (recorded:
  "columns in the order of the statement"). So rule 1 of D18 applies. A
  result with no rows has both, and `rows` is empty (recorded: "no rows").
- Each row is a JSON object whose keys are the names of `columns`, in that
  order (recorded: "columns in the order of the statement").
- Two columns with one name get the names `a` and `a0`, so each key is
  unique (recorded: "two columns with one name" and "two columns of a join
  with one name"). A column with no name is `EXPR$0`, `EXPR$1` and so on
  (recorded: "a column with no name").
- `queryState` is `COMPLETED`, `FAILED` or `CANCELED` (recorded: "a
  statement", "a syntax error" and "a long query to cancel").
- A statement that is not a query answers rows too. `DROP TABLE`, `CREATE
  VIEW`, `USE` and `ALTER SESSION` give the columns `ok` and `summary`
  (recorded: "crud: drop the table again" and "feature: use"). `CREATE TABLE
  AS` gives `Fragment` and `Number of records written` (recorded: "crud:
  create table as").
- `metadata` names the type of the first batch of rows. A column whose type
  changes between two files keeps the name of the first type, and each value
  of the second type arrives as `null`, in a result that ends `COMPLETED`.
  `typeof` names the second type for those rows (recorded: "a type that
  changes between files", "the type of each value of a type that changes"
  and "the second file alone"). The option `exec.enable_union_type` changes
  nothing (recorded: "a type that changes, with the union type"). The same
  files with `ORDER BY` give no rows and `FAILED`, with no message (recorded:
  "a type that changes, sorted").
- A result has no cap by default. `employee.json` gave all of its 1155 rows
  (recorded: "a result of 1155 rows").
- `autoLimit` caps the rows, and `attemptedAutoLimit` names the cap
  (recorded: "autoLimit of 100" and "autoLimit as a number"). It can be a
  string or a JSON number. A value of `-1`, or a string that is not a number,
  is no cap, with `attemptedAutoLimit` 0 (recorded: "autoLimit of -1" and
  "autoLimit that is not a number").
- `attemptedAutoLimit` names the cap even when the statement gives fewer
  rows. `LIMIT 3` with `autoLimit` 10 gave 3 rows and `attemptedAutoLimit`
  10 (recorded: "autoLimit above a LIMIT"). So a result with as many rows as
  the cap does not say whether rows were left out.
- The session option `exec.query.max_rows` caps the rows too, and then
  `attemptedAutoLimit` is 0. So the answer gives no sign of that cut
  (recorded: "the option exec.query.max_rows of 7"). With both, the smaller
  one holds, and `attemptedAutoLimit` names `autoLimit` (recorded: "autoLimit
  and exec.query.max_rows").
- There is no paging. A result is one body. `LIMIT` and `OFFSET` in the SQL
  work (recorded: "lead: a page with OFFSET").
- An answer has no `Content-Encoding` (recorded: "a gzip answer"). The only
  redirect is the answer to a wrong password (recorded: "a wrong password").

These facts were measured with `curl` on 2026-10-01:

- The status and the headers come only when the first batch of rows is
  ready. For a `COUNT(*)` over a cross join that a cancel ended after 1.5
  seconds, the first byte came when the query ended. So the client learns
  the `queryId` of a statement only with its first batch.

These facts come from the REST source, and are not measured:

- The server streams each batch as it comes, and drops each row after the
  cap of `autoLimit` or `exec.query.max_rows`.
- The metadata of a column is its minor type, with the precision and the
  scale, or the width, and never its mode. So a list and a column that can
  hold NULL look like any other column (see Types).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table on
2026-10-02, from the recordings. Ken has not reviewed it yet, and no code
exists, so step 10 will write it again from the code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| INT | integer | `int64` | `int64` | `INT` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| FLOAT4 | float | `float64, of the float32 that the server widens` | `float64` | `FLOAT4` | yes |
| FLOAT8 | float | `float64, with the infinities and NaN from their strings` | `float64` | `FLOAT8` | yes |
| VARDECIMAL | decimal | `*apd.Decimal, from the digits of the JSON number` | `*apd.Decimal` | `VARDECIMAL` | yes |
| BIT | boolean | `bool` | `bool` | `BIT` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| VARBINARY | binary | `[]byte, from base64` | `[]uint8` | `VARBINARY` | yes |
| DATE | date | `dbimp.Date, from the milliseconds since 1970-01-01 in UTC` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime, from the milliseconds since midnight` | `dbimp.LocalTime` | `TIME` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime, from the milliseconds since 1970-01-01 in UTC` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| INTERVAL | interval | `dbimp.Interval, from its ISO 8601 text` | `dbimp.Interval` | `INTERVAL` | yes |
| INTERVALDAY | interval | `dbimp.Interval, from its ISO 8601 text` | `dbimp.Interval` | `INTERVALDAY` | yes |
| INTERVALYEAR | interval | `dbimp.Interval, from its ISO 8601 text` | `dbimp.Interval` | `INTERVALYEAR` | yes |
| MAP | map | `map[string]any, of the decoded JSON values` | `map[string]interface {}` | `MAP` | yes |
| LIST | array | `[]any, of the decoded JSON values of its elements` | `[]interface {}` | `LIST` | yes |
<!-- /dbimp:types -->

Each type of this table differs from the Go type of its kind in no row. Two
facts below need Ken's decision before the table is final: a list that
arrives under the name of its element, and the NULL that a change of type
gives. The open questions name both.

These facts were recorded on each release, from a Parquet table of every
type with a row of the smallest values, a row of the largest values and a
row of NULL (recorded: "every type"), and from literals (recorded: "every
type as a literal"):

- `metadata` names a type with its precision and scale, or its width, such
  as `VARDECIMAL(38, 18)`, and `VARCHAR(1)` for the literal `'x'`. A column
  of a file has no width, such as `VARCHAR`. `FLATTEN` gave `BIGINT(0, 0)` (recorded:
  "feature: flatten").
- A NULL is JSON `null`, for every type. An untyped `NULL` is named `INT`
  (recorded: "every type as a literal").
- `INT` holds -2147483648 to 2147483647. `BIGINT` keeps every digit of
  -9223372036854775808 and 9223372036854775807. Both are JSON numbers.
- `FLOAT4` is a JSON number of the float32, widened, so -3.4028235E38 arrives
  as -3.4028234663852886E38 and 1.4E-45 as 1.401298464324817E-45. `FLOAT8`
  keeps -1.7976931348623157E308. An infinity and NaN of `FLOAT8` are the
  strings `"Infinity"`, `"-Infinity"` and `"NaN"` (recorded: "an infinity
  and NaN").
- `VARDECIMAL` is a JSON number with every digit, such as
  -12345678901234567890.123456789012345678 for `DECIMAL(38, 18)`, and
  12345678901234567890123456789012345678 for `DECIMAL(38, 0)` (recorded: "a
  decimal of 38 digits"). A small value can come in the form of an exponent:
  0.000000000000000001 arrives as `1E-18`, and
  -0.00000000000000000000000000000000000001 as `-1E-38`. Each `DECIMAL`
  of the SQL becomes `VARDECIMAL`.
- `BIT` is a JSON boolean, and `TRUE` is named `BIT`.
- `VARCHAR` is a JSON string, and keeps `é`, quotes and backslashes. An
  empty string stays empty. `CAST(x AS CHAR(5))` is named `VARCHAR(5)`, is
  not padded, and cuts a longer text to 5 characters (recorded: "a CHAR
  column").
- `VARBINARY` is a string in base64, such as `"YWI="`, and an empty value is
  `""`. `CAST(x AS BINARY(2))` is named `VARBINARY` (recorded: "BINARY").
- `DATE` is the milliseconds since 1970-01-01 in UTC, such as 1790812800000
  for 2026-10-01. 0001-01-01 is -62135596800000 and 9999-12-31 is
  253402214400000, which are days of the Gregorian calendar.
- `TIME` is the milliseconds since midnight, such as 45296789 for
  12:34:56.789, and 86399999 for 23:59:59.999. So it keeps milliseconds.
- `TIMESTAMP` is the milliseconds since 1970-01-01 of the time read as UTC.
  `TIMESTAMP '2026-10-01 12:34:56.789123'` arrives as 1790858096789, so the
  microseconds are lost. 1969-12-31 23:59:59.999 is -1, and 9999-12-31
  23:59:59.999 is 253402300799999. The zone of the server is UTC, and
  `NOW()` equals `LOCALTIMESTAMP` (recorded: "the zone of the server").
- `INTERVALYEAR` and `INTERVALDAY` are the text of ISO 8601, such as
  `"P1Y2M"` and `"P1DT7384.500S"` for `INTERVAL '1 2:03:04.5' DAY TO
  SECOND`, which keeps the hours, the minutes and the seconds as seconds.
  A negative interval has a sign on each part, such as `"P-1Y-2M"` and
  `"P-1DT-7384.500S"`. Zero is `"PT0S"` (recorded: "every type").
- Both kinds of interval, read back from the Parquet file, are named
  `INTERVAL`, and `INTERVAL '-1-2' YEAR TO MONTH` reads back as `"P-14M"`
  (recorded: "every type").
- `MAP` is a JSON object, such as `{"k":[1,2,3],"o":{"s":"x"}}`, and `LIST`
  is a list of lists, such as `[[1,2],[3]]` (recorded: "complex values").
- A list of a scalar type, or of maps, has no name of its own. `[1,2,3]`
  arrives under `BIGINT`, `["a","b"]` under `VARCHAR`, and `[{"a":1}]` under
  `MAP`. An empty list arrives as `null`, under `INT` (recorded: "complex
  values" and "a list of strings"). A list cannot hold a NULL (recorded: "a
  list with a null").
- `modeof` names such a column `ARRAY`, and other columns `NULLABLE`.
  `sqlTypeOf` names the list of `BIGINT` as `BIGINT` too, and a `MAP` as
  `STRUCT` (recorded: "lead: the mode and the type of a list"). `sqlTypeOf`
  names `INT` as `INTEGER` and each interval as `INTERVAL` (recorded: "lead:
  the SQL type of every column").
- `INFORMATION_SCHEMA.COLUMNS` has no rows for a Parquet file, and names
  each column of a view `ANY` (recorded: "feature: the columns of a table"
  and "schema: the columns of a view").

These types fail (recorded):

- `SMALLINT`, `TINYINT` and `REAL` fail with `UnsupportedDataTypeException`
  and `See Apache Drill JIRA: DRILL-1959` ("SMALLINT", "TINYINT" and "the
  names BOOLEAN, INTEGER, DOUBLE and REAL").
- `UINT1`, `UINT2`, `UINT4` and `UINT8` are unknown names ("an 8-bit
  unsigned integer" to "a 64-bit unsigned integer").
- `TIMESTAMP WITH LOCAL TIME ZONE` fails when the server plans it, and `TIME
  WITH TIME ZONE` is a syntax error ("a timestamp with a zone" and "a time
  with a zone").
- No statement gave a `DICT`. A schema that names `MAP<VARCHAR, BIGINT>`
  for a column still gives `MAP` (recorded: "a dict from a provided schema"
  and "read the dict of the provided schema"). No statement gave a
  `UNION`, with or without `exec.enable_union_type` (recorded: "the union
  type" and "a type that changes, with the union type").

These facts come from the sources, and are not measured:

- The native protocol has the types `DECIMAL28SPARSE`, `DECIMAL38SPARSE`,
  `SMALLINT`, `TINYINT` and `UINT1` to `UINT8` (the Go driver). The REST
  answer named none of them.
- The Python driver reads `DATE`, `TIME` and `TIMESTAMP` as the
  milliseconds since 1970-01-01 from Drill 1.19, and as text before it.

## Parameters

These facts were recorded on each release, as both principals:

- The server binds no parameters. `?` fails with `Illegal use of dynamic
  parameter`, `:p` is a syntax error, and `$1` is a column that does not
  exist (recorded: "a placeholder", "a named placeholder" and "a numbered
  placeholder").
- The body has no key for parameters. `params` fails with HTTP 400
  (recorded: "lead: a body key for parameters"). `PREPARED_STATEMENT` as the
  `queryType` fails with no message (recorded: "lead: a prepared statement
  as the query type").
- So, by item 4 of step 6, a driver binds arguments with the parser for
  placeholders of the root package (D34). Step 9 decides how.

These facts come from the sources, and are not measured:

- The native protocol has prepared statements (the Go driver). The REST
  interface has none (the REST source).

## Transactions

These facts were recorded on each release, as both principals:

- Drill has no transactions. `START TRANSACTION`, `BEGIN`, `COMMIT` and
  `ROLLBACK` each fail with `Non-query expression encountered in illegal
  context` (recorded: "a transaction", "lead: begin", "a commit" and "lead:
  rollback"). The manifest names item 11 as absent for that reason.
- Drill writes only with `CREATE TABLE AS`, into a workspace that can be
  written. `INSERT` fails with `Storage plugin [dfs] is immutable or doesn't
  support inserts`. `UPDATE`, `DELETE` and `MERGE` fail with a
  `ClassCastException` (recorded: "crud: create table as", "crud: insert",
  "crud: update", "crud: delete" and "crud: merge").

## Errors

These facts were recorded on each release, as both principals:

- An error before any row answers HTTP 200 with
  `{"queryId":"...","queryState":"FAILED"}`, and no message (recorded: "a
  syntax error").
- The session option `drill.exec.http.rest.errors.verbose` set to `"true"`
  in `options` adds `exception`, the name of the Java class, `errorMessage`
  and `stackTrace` to that answer, for both principals (recorded: "a syntax
  error, verbose" and "an unknown table"). An error of Drill starts with its
  kind, such as `VALIDATION ERROR:`, `PERMISSION ERROR:` or `PLAN ERROR:`,
  and ends with `[Error Id: ...]`. An error of the parser names the line and
  the column. A division by zero of two literals fails when the server plans
  it, with `Error while applying rule ReduceAndSimplifyProjectRule`
  (recorded: "a division by zero").
- An error after some rows ends the array of rows, then sends
  `"queryState":"FAILED"`, with HTTP 200 and no message, even with the
  verbose option (recorded: "an error after some rows" and "an error after
  some rows, verbose"). The body is whole JSON. The rows before the error
  arrive: 300 rows from the first file, then the error in the second.
- `GET /profiles/{queryId}.json` gives the profile of a query, with `error`,
  such as `SYSTEM ERROR: Drill Remote Exception`, and `verboseError`, which
  holds the cause, such as `(java.lang.NumberFormatException) x`, and the
  Java stack (recorded: "the profile of the query that failed after some
  rows"). The ordinary user reads the profile of its own query.
- A profile that does not exist answers HTTP 500 with `{ 'message' : 'error
  (unable to serialize profile)' }`, which is not JSON (recorded: "a profile
  that does not exist").
- A request that the server cannot start answers HTTP 500 with
  `{"errorMessage" : "Query submission failed"}` and no reason: a schema that
  does not exist, an option that does not exist, and `userName` (recorded,
  under Requests).
- A body that the server cannot read answers HTTP 400 in plain text, and a
  wrong content type HTTP 500 (recorded, under Requests).
- A wrong password answers HTTP 307 (recorded: "a wrong password"). No
  request got HTTP 429 or HTTP 503.

These facts were measured with `curl`:

- The ordinary user reading the profile of a query of `admin` got the same
  answer as a profile that does not exist (measured on 2026-10-01).

These facts come from the REST source, and are not measured:

- The answer to an error after some rows cannot carry a message, because
  the server writes `errorMessage` only when no batch was sent (a comment in
  `StreamingHttpConnection.java`).

## Cancellation and timeouts

These facts were recorded on each release, as both principals:

- A query keeps running when the client leaves. A cross join that the
  client left after 1 second was still in `runningQueries` of `GET
  /profiles.json` 2 seconds later, and ended `Succeeded` about 13 seconds
  after it started (recorded: "the running queries after the client left"
  and "the running queries after the left query ended").
- `GET /profiles.json` lists the running and the finished queries, each with
  its `queryId`. The ordinary user sees only its own queries (recorded: "the
  running queries", as the ordinary user).
- `GET /profiles/cancel/{queryId}` stops a running query. It answers HTTP
  200 with `Cancelled query ... on locally running node.` in plain text, and
  the answer of the query then ends with no rows and `"queryState":
  "CANCELED"` (recorded: "cancel the running query" and "a long query to
  cancel"). The ordinary user can cancel its own query.
- `GET /query/{queryId}/cancel` does not exist (recorded: "lead: another
  endpoint to cancel").

These facts were measured with `curl` on 2026-10-01:

- The ordinary user cannot cancel a query of `admin`. It gets HTTP 500 with
  `PERMISSION ERROR: Not authorized to cancel the query`.
- The `queryId` is the first member of the answer, but the answer starts
  only with the first batch (see Responses). So a client that waits for its
  first row does not know the id of its own query. It can find it in `GET
  /profiles.json` by its user and the text of its query.

## Statements

These facts were recorded on each release, as both principals:

- A request runs one statement. Two statements fail with `Encountered ";"`
  (recorded: "two statements").
- A statement that ends with a semicolon fails the same way (recorded: "a
  statement that ends with a semicolon"). An empty statement fails with
  `Encountered ""` (recorded: "an empty statement").
- Comments with `--` and `/* */` are part of the text (recorded:
  "comments").
- `DROP TABLE IF EXISTS` of a table that does not exist answers `ok` false
  and `COMPLETED` (recorded: "schema: drop a table that does not exist").
- `CREATE TABLE` with a list of columns fails with `Extended columns not
  allowed under the current SQL conformance level`, and so does a primary
  key, a foreign key and a unique constraint. `CREATE INDEX` is a syntax
  error (recorded: "schema: create table", "schema: primary key", "schema:
  foreign key", "schema: unique constraint" and "schema: index").
- `CREATE VIEW`, `CREATE TEMPORARY TABLE`, `CREATE TABLE ... PARTITION BY`,
  `ANALYZE TABLE`, `REFRESH TABLE METADATA`, `SHOW SCHEMAS`, `SHOW FILES`,
  `USE`, `EXPLAIN PLAN FOR` and a table function each run (recorded: the
  requests that start with "schema:" and "feature:").
- `CREATE OR REPLACE SCHEMA ... FOR TABLE` gives a JSON table a schema. A
  column with `NOT NULL DEFAULT 'z'` that the files lack still reads as
  `null` (recorded: "schema: default value" and "schema: default value
  read").
- `DESCRIBE` of a Parquet table gives its columns `COLUMN_NAME`,
  `DATA_TYPE` and `IS_NULLABLE`, and no rows (recorded: "feature:
  describe").

These facts come from the REST source, and are not measured:

- The web form of the server cuts a semicolon at the end of a statement.
  `/query.json` does not.

## Principals

These facts were recorded on each release:

- `dbmeta_user` can query each storage plugin, make and drop a table and a
  view in `dfs.tmp`, set a session option, and read `/options.json` and
  `/profiles.json` (recorded as `dbmeta_user`).
- `dbmeta_user` cannot change a system option. `ALTER SYSTEM SET` and
  `ALTER SYSTEM RESET` fail with `PERMISSION ERROR: Not authorized to change
  SYSTEM options.` `admin` can (recorded: "alter system" and "reset a system
  option").
- `GET /storage.json` gives the storage plugins to `admin`, and HTTP 500
  with `User not authorized.` to `dbmeta_user` (recorded: "the storage
  plugins").
- `SELECT version FROM sys.version` gives the version to both users, such as
  `1.22.0` (recorded: "the version"). The Python driver reads `SELECT
  MIN(version) AS version FROM sys.drillbits`, which gives the same
  (recorded: "the version as the Python driver reads it").

## Flavors

- No other product speaks this interface (Gemini and DeepSeek, not
  measured). Dremio began as a fork of Drill, and its REST interface is
  `/api/v3/sql` with jobs, which differs (Gemini and DeepSeek, not
  measured). DeepSeek said that the Drill of MapR is the same code (not
  measured).

## Interfaces

Step 10 writes this table from the code. No code exists yet.

## Faults

`usql` has no driver for Drill, so no fault of a driver that `usql` uses
applies. These are faults of the other drivers that a driver here does not
repeat:

- The Go driver puts the milliseconds of a `TIMESTAMP` into the nanoseconds
  of a `time.Time`, as `time.Unix(ts/1000, ts%1000)`, drops the milliseconds
  of a `TIME`, and reads a decimal as a `float64` (`internal/data`).
- The Go driver returns `driver.ErrBadConn` when it cannot submit a query,
  which can send a query twice, and connects with `context.Background`
  (`driver/conn.go`).
- The Python driver gives no message for an error when the server sends
  none, and does not ask for the verbose errors (`_drilldbapi.py`).
- The Python driver tells the releases apart by comparing the version as a
  string, `self.drill_version < '1.19'` (`_drilldbapi.py`).

## Second opinions

Step 7 asked Gemini and DeepSeek on 2026-10-02 what step 6 did not find, and
sent each lead to the server:

- Parameters. Both said that the REST interface binds none. DeepSeek named
  the body keys `params`, `parameters` and `prepared`, and Gemini the query
  type `PREPARED_STATEMENT`. The server refuses `params` with HTTP 400 and
  fails `PREPARED_STATEMENT` (recorded: "lead: a body key for parameters"
  and "lead: a prepared statement as the query type").
- Transactions. Both said none. `BEGIN` and `ROLLBACK` fail (recorded:
  "lead: begin" and "lead: rollback").
- Paging. Both said that only `LIMIT` and `OFFSET` exist. `OFFSET` works
  (recorded: "lead: a page with OFFSET").
- A list and NULL. DeepSeek said that the metadata can hold a mode, and
  Gemini that `modeof` names it. The metadata holds no mode (recorded:
  "complex values"). `modeof` names a list `ARRAY` (recorded: "lead: the
  mode and the type of a list"). Gemini said that a schema that a statement
  provides keeps the lists. It was not tested.
- The id of a query. DeepSeek said that a header can carry it. No answer
  has one (recorded: "a statement"). Both named `GET /profiles.json`, which
  lists it (recorded: "the running queries").
- Cancel. Both named `GET /profiles/cancel/{queryId}`, which works, and
  `GET /query/{queryId}/cancel`, which does not exist (recorded: "cancel the
  running query" and "lead: another endpoint to cancel").
- An error after some rows. Both said that the profile holds the message.
  It does, in `error` and `verboseError` (recorded: "the profile of the
  query that failed after some rows").
- Flavors. Both said that none speaks this interface (not measured).

Step 5a asked both models four questions on 2026-10-01. Gemini said that
`INSERT` works on Iceberg, Delta Lake and JDBC, and that `CREATE SCHEMA`
exists. DeepSeek said that `INSERT` works on `dfs`. The server refuses
`INSERT` on `dfs` (recorded: "crud: insert"), and no other plugin can be
written in the image. Gemini said that a `DATE` and a `TIMESTAMP` are text,
and that a `BIGINT` and a `VARDECIMAL` can be strings. Each is a JSON
number (recorded: "every type"). DeepSeek named `TINYINT` and `SMALLINT`,
which the server refuses.

Step 8a asked both models on 2026-10-02 to review the mapping of the types:

- Gemini agreed with each row, and said that a `TIMESTAMP` is a local
  timestamp, because Drill has no zone, and `NOW()` equals
  `LOCALTIMESTAMP`.
- Gemini said that a list under the name of its element gives a `[]any` of
  the Go type of the element, and that `ColumnTypeScanType` names `any` for
  such a column, because the scalar type does not match a list.
- Gemini said that an empty list, which arrives as `null`, is nil, and that
  the `null` that a change of type gives is nil too, as the server sends it.
- `deepseek-flash` timed out three times on 2026-10-02, so the same brief
  went to `deepseek-v4-pro`. It agreed with each row, said that a
  `TIMESTAMP` is a local timestamp, that a list under the name of its
  element gives a `[]any` and that `ColumnTypeScanType` names `any`, and
  that an empty list and the `null` of a change of type are each nil, and
  not an error.

## Open questions

Each one waits for Ken, with step 9:

1. The server refuses `INSERT`, `UPDATE` and `DELETE`, and writes only with
   `CREATE TABLE AS` into `dfs.tmp` (recorded: "crud: insert", "crud:
   update" and "crud: delete"). Step 6 says to ask Ken when a server refuses
   one of the four. Each test of step 14a then makes a table with `CREATE
   TABLE AS`, reads it, and drops it.
2. A list of a scalar type arrives under the name of its element, such as
   `BIGINT`, because the metadata holds no mode (recorded: "complex
   values"). A driver can take the shape from the JSON token, and give a
   `[]any`, but `ColumnTypeScanType` cannot know it before the first row.
3. A column whose type changes between two files gives `null` for each
   value of the second type, with `COMPLETED` (recorded: "a type that
   changes between files"). That is a wrong NULL, which hard rule 3 forbids,
   and the answer gives no sign of it. Only `typeof` in the statement shows
   it.
4. An error after some rows has no message in the answer. The message is in
   the profile, which costs a second request (recorded: "the profile of the
   query that failed after some rows").
5. An error before any row has a message only with the option
   `drill.exec.http.rest.errors.verbose` in each request (recorded: "a syntax
   error, verbose").
6. `exec.query.max_rows` cuts a result with no sign, and a result as long as
   `autoLimit` does not say whether rows were cut (recorded: "the option
   exec.query.max_rows of 7" and "autoLimit above a LIMIT"). This touches
   D21, though only a caller who sets either one meets it.
7. A query goes on when the client leaves. To stop it, a driver needs its
   `queryId`, which comes only with the first batch, or from `GET
   /profiles.json` (recorded: "the running queries after the client left").
8. Each request with basic authentication and no cookie makes a session on
   the server, which lives for 3600 seconds when idle (measured, under
   Requests). Whether a driver keeps the cookie is a choice of step 9.
