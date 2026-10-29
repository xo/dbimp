# MaxCompute

This file holds what is known about Alibaba Cloud MaxCompute (formerly ODPS), for
the driver `maxcompute` (W47). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, or "not measured", with its source. Step 3 read the source of
`github.com/aliyun/aliyun-odps-go-sdk` v0.4.26, which is the module that `usql`
imports. Step 6 recorded the rest on 2026-10-29, as one principal, on the project `dbimp` in
ap-southeast-5 (Jakarta). A recorded fact has its request in `testdata/maxcompute/requests.json`
under that name, and the exchange in the file that `testdata/maxcompute/manifest.json` names.
Account numbers in the files are written as `<account>` and `<owner>`.

## Summary

- Product: MaxCompute, a hosted data warehouse of Alibaba Cloud. Name in `dbrun`: `maxcompute`
  (from `dbmeta/hosted/hosted.go`). The `dbsetup` session made the account of the recordings,
  a project in ap-southeast-5.
- R: a hosted service has no container and no emulator, so `dbrun` cannot start it. This is
  the condition of Snowflake (D183 item 13). The integration tests run only where a person
  supplies an account.
- H: a REST API over HTTPS. A statement is an XML document, and the answer is XML with the
  result as CSV text (recorded: "select 1"). The binary protocol of the Tunnel is not used
  (decided by Ken).
- S: a SQL dialect that is close to Hive SQL. A person can type it (recorded: "select 1").
- Scheme in `dburl`: `Name` `maxcompute`, alias `mc`, generator `GenMaxCompute`, `GoPackage`
  `github.com/aliyun/aliyun-odps-go-sdk/sqldriver` (read, `dburl/scheme.go`).
- Driver that `usql` uses now: the SDK, v0.4.26. `usql` registers it as `maxcompute` in
  `drivers/maxcompute/maxcompute.go`, and `drivers.Driver` is empty, so there is no `Version`
  statement.

## Requests

Recorded ("select 1"). A statement is a SQL task, and it takes three calls.

1. `POST /api/projects/<project>/instances?curr_project=<project>` with `Content-Type:
   application/xml` and the body `<Instance><Job><Priority>9</Priority><Tasks><SQL><Name>..</Name>
   <Config>..</Config><Query>..;</Query></SQL></Tasks></Job></Instance>`. The query must end with
   a semicolon ("select with a trailing semicolon missing"). The `Config` holds the property
   `type` with the value `sql`, and the property `settings`, a JSON object of hints ("select with
   settings in the task"). A `<Task Type="SQL">` element answers `400 SchemaParseError`.
2. A query answers `201`, and the `Location` header names the instance. A statement of DDL, `DESC` or
   `SHOW` answers `200` with the result in the body and no `Location`.
3. `GET <instance>?taskstatus` answers `Running`, then `Success`, `Failed` or `Cancelled`. `GET
   <instance>?result` answers the result.

The request is signed with V4 and the region of the host. The headers are `Date` and
`Authorization: ODPS <id>/<yyyymmdd>/<region>/odps/aliyun_v4_request:<base64 HMAC-SHA1>`. The key
is an HMAC-SHA256 chain over `aliyun_v4` and the secret, the date, the region, `odps` and
`aliyun_v4_request`. The text to sign is the method, `Content-Md5`, `Content-Type`, `Date`, the
sorted `x-odps-` headers, the path without `/api`, and the sorted query. The Jakarta endpoint
resets many connections, so the client must send a statement again when no byte arrived.

## The DSN

The DSN of the SDK is `http://<access id>:<access key>@<host>:<port>/<path>?project=<project>`,
with the optional keys `stsToken`, `tcpConnectionTimeout`, `httpTimeout`, `tunnelEndpoint`,
`tunnelQuotaName` and `enableLogview`. Any other key becomes a hint. `project` is required,
and the user and password fall back to `ALIBABA_CLOUD_ACCESS_KEY_ID` and
`ALIBABA_CLOUD_ACCESS_KEY_SECRET` (read, `sqldriver/dsn.go`). This breaks hard rule 2, because
the library reads the environment. The new driver does not (D7). `dbmeta` uses the form
`maxcompute://<key id>:<secret>@service.<region>.maxcompute.aliyun.com/api?project=<project>`
(read, `dbmeta/hosted/hosted.go`). The new DSN is the one of D218.

## Responses

Recorded. A `SELECT` gives `Result Format="csv"`, and `DESC`, `SHOW`, `EXPLAIN` and DML give
`Format="text"` ("select 1", "describe the table of types"). A result with a special character
has `Transform="Base64"`. A CSV line ends with CRLF. The first line names the columns, and a
repeated name `b` becomes `b2` and an unnamed column is `_c0` ("columns in order and a repeated
name", "no column name"). The CSV holds no type of a column, and no call returns a schema.
A NULL is `"\N"`. A number and a boolean are not quoted, and a DOUBLE is `1.0`. DECIMAL, DATE,
TIMESTAMP, STRING and BINARY are quoted, and DATETIME is not. A CHAR is padded. A BINARY is text
with `=00=FF` escapes. An array, a map and a struct are text that cannot be parsed back, as in
`"[a,b, c""d, NULL]"` ("type array: low, high and NULL"). The result holds at most 10,000 rows
with no sign: 9,999 and 10,000 rows come whole, and 10,001 and 25,000 rows come as 10,000 ("9999
rows", "10000 rows", "10001 rows"). The Tunnel and Arrow are not used (decided by Ken).

## Types

The result CSV carries no type, and the statement `EXPLAIN OUTPUT <query>;` answers the name and the exact
type of each column as text ("lead: explain output of every type"): `TINYINT`, `SMALLINT`, `INT`, `BIGINT`,
`FLOAT`, `DOUBLE`, `DECIMAL(38,18)`, `BOOLEAN`, `STRING`, `VARCHAR(5)`, `CHAR(5)`, `BINARY`, `DATE`,
`DATETIME`, `TIMESTAMP`, `TIMESTAMP_NTZ`, `ARRAY<INT>`, `MAP<STRING,BIGINT>`, `struct<x:BIGINT>`, `JSON`,
`INTERVAL_DAY_TIME`, `INTERVAL_YEAR_MONTH` and `VOID` for the literal NULL. It names a repeated column as it
is (`b`, `b`) and an unnamed column as `_c3`, where the CSV header has `b2` and `_c0`, so the driver takes the
names from the CSV and the types by position ("lead: explain output with a repeated name and no name"). A
statement that is not a query, or that has an error, answers the error as text ("lead: explain output of a
missing table"). Ken decided on 2026-10-29 that the driver sends it before each query and maps each cell by
its type (see "Decided by Ken after step 7").

The cell forms are the ones of the section Responses. The mapping of step 8a is below. Ken reviewed it on
2026-10-29, and `TestTables` will make the same table from the code at step 10.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INT | integer | `int64` | `int64` | `INT` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| FLOAT | float | `float64` | `float64` | `FLOAT` | yes |
| DOUBLE | float | `float64` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `*apd.Decimal` | `*apd.Decimal` | `DECIMAL` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| CHAR | string | `string` | `string` | `CHAR` | yes |
| BINARY | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| DATETIME | timestamp | `time.Time` | `time.Time` | `DATETIME` | yes |
| TIMESTAMP | timestamp | `time.Time` | `time.Time` | `TIMESTAMP` | yes |
| TIMESTAMP_NTZ | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP_NTZ` | yes |
| INTERVAL_DAY_TIME | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL_DAY_TIME` | yes |
| INTERVAL_YEAR_MONTH | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL_YEAR_MONTH` | yes |
| JSON | json | `any` | `interface {}` | `JSON` | yes |
| ARRAY | other | `string` | `string` | `ARRAY` | yes |
| MAP | other | `string` | `string` | `MAP` | yes |
| STRUCT | other | `string` | `string` | `STRUCT` | yes |
<!-- /dbimp:types -->

The notes:

- A `CHAR` cell is padded to its length (`"ab   "`). The driver keeps the padding, as the server gives it.
- A `BINARY` cell is text with `=XX` escapes (`"=00=FF=10"`, and `"abc"` for plain letters). The escape rules need
  a round trip at step 14a, because the bytes `=` and the quote are not recorded.
- `DATETIME` and `TIMESTAMP` cells have no zone in their text, and the project has a time zone (the setting
  `odps.sql.timezone`, recorded in "timezone"). Ken decided that the driver gives a `time.Time` in the zone of the project (decision 3 above).
- An `ARRAY`, a `MAP` and a `STRUCT` cell is text that cannot be parsed back (`"[a,b, c""d, NULL]"`), so the
  driver gives the text of the server, which the kind `other` names (decided by Ken).
- A string of a backslash and an N arrives as the cell `""\N""`, and a NULL arrives as `"\N"`, so the two differ
  ("lead: a string of a backslash and N, which the models say is quoted as a string"). Gemini said that a quoted
  `"\N"` is a string, which is wrong: a NULL is quoted too.

## Parameters

None. The server binds nothing: `?` is a parse error and `@a` is not resolved ("a question mark
parameter", "a named parameter"). The driver writes each argument into the text with the parser
for placeholders in the root package (D34), as the SDK driver does.

## Transactions

None. `BEGIN` and `COMMIT` are refused ("a transaction statement"). `UPDATE`, `DELETE` and
`MERGE` work only on a transactional table, which has a primary key and the property
`transactional`. There is no `UPSERT` ("update on a plain table", "update the transactional
table", "upsert"). `BeginTx` returns a named error (D20).

## Errors

A statement that does not parse answers `400` with an XML `Error`, such as `ParseError` with
`ODPS-0130161` ("a syntax error"). A statement that parses and then fails, such as a missing
table, answers `201` or `200`, and the task `Status` is `Failed` with the message in a `Result
Format="text"` ("an error before any rows: a missing table"). A wrong secret is `403
SignatureNotMatch` with `ODPS-0410042`, and no credentials are `403 MissingArgument` ("a wrong
secret", "no credentials"). A missing project or instance is `404 NoSuchObject`.

## Cancellation and timeouts

`PUT <instance>` with `<Instance><Status>Terminated</Status></Instance>` answers `202`, and the
task then reads `Cancelled` with `ODPS-0424171` ("cancel: terminate it", "cancel: the result
after the terminate"). The driver sends it when the context ends.

## Statements

A request holds one statement. Two statements need the setting `odps.sql.submit.mode` with the
value `script`, and the caller sets it. The driver never counts the statements of a query (decided
by Ken). Without it the server answers `400` ("set flags in the text"). With it a `SET` and a
`SELECT` work, and two `SELECT` statements fail with `ODPS-0130071`, which allows one statement that
prints ("two statements in script mode", "a set statement in script mode"). A comment is accepted
("a line comment", "a block comment"). The project has no schema ("a schema"). The version is
`SELECT version()`, which gives text.

## Principals

One principal, the key `dbimp-alicloud`. `dbmeta` names the keys `ALIBABA_CLOUD_ACCESS_KEY_ID`,
`ALIBABA_CLOUD_ACCESS_KEY_SECRET` and `ALIBABA_CLOUD_SECURITY_TOKEN`, which a person sets. The
library reads none (D7).

## Flavors

None known.

## Interfaces

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and signs each request with the key of the DSN for the host of the DSN only (D218 and D221). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping reads the project, which checks the key and the project and costs no query. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each statement is its own instance (recorded). |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver writes as literals of their own: a decimal, a dbimp.Date, a dbimp.LocalDateTime, a dbimp.Interval, and a list and a map that the binding refuses with dbimp.ErrArguments (D220). |
| `driver.QueryerContext` | yes | The driver sends EXPLAIN OUTPUT for the types and then the statement, with its arguments written into its text, and reads the CSV (D219 and D220). |
| `driver.ExecerContext` | yes | Exec sends the statement and reads no result. RowsAffected is an error that wraps dbimp.ErrNotSupported, because the server gives no count (D178 and D220). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the server has no transaction (D20 and D220). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result. |
| `driver.RowsColumnTypeScanType` | yes | EXPLAIN OUTPUT names the type of each column, and each type has one Go type (D135 and D219). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The name of the type in upper case, with no parameters, such as DECIMAL. |
| `driver.RowsColumnTypeLength` | yes | The length of a VARCHAR or a CHAR column, from the text of its type. The other types have none. |
| `driver.RowsColumnTypeNullable` | yes | EXPLAIN OUTPUT has no nullability, so every column can be NULL and the second result is false (recorded). |
| `driver.RowsColumnTypePrecisionScale` | yes | A DECIMAL column has its precision and its scale in the text of its type. |
<!-- /dbimp:interfaces -->

## Faults

The faults of the SDK driver, read in v0.4.26. The driver of this repository has none of them, and a test holds each
difference.

1. `Begin` and `Prepare` return `nil, nil` instead of an error. Here `BeginTx` returns `dbimp.ErrNotSupported`, and a
   prepared statement runs as its text (`TestTransactionIsRefused`).
2. `exec` returns a `nil` `driver.Result`, so a call to `RowsAffected` crashes. Here `RowsAffected` returns an error that
   wraps `dbimp.ErrNotSupported` (`TestExecOfDDLRunsAtOnce`).
3. `QueryContext` and `ExecContext` ignore `ctx`. Here the context stops the poll, and the driver ends the instance on the
   server (`TestExecSendsAStatementOnceAndEndsItOnTheServer`, `TestIntegrationDeadlineEndsTheStatement`).
4. It reads `ALIBABA_CLOUD_ACCESS_KEY_ID` and the other variables from the environment. Here the DSN holds the key
   (D218).
5. The Tunnel call reads the whole result before the first row (`recordCount`), and an empty result sets the count to 1
   to avoid an error. Here the CSV is read one cell at a time (`TestContract`, "close before the end reads nothing more").
6. `enableLogview` writes to the standard logger. Here the driver logs nothing (D7).

The faults of the server that the driver cannot remove, all recorded or measured on 2026-10-29:

1. The server cuts a result at 10,000 rows with no sign. The caller pages with `ORDER BY` and `LIMIT <offset>, <count>`
   (`TestIntegrationCapOfTheResult`).
2. A `DATE` of the year 1 is read two days later than it was written: `1900-01-01` and `9999-12-31` come back whole
   (`TestIntegrationRoundTrip/DATE`, which writes `1900-01-01` for this reason).
3. A statement of DML gives no count of rows, so `RowsAffected` is an error.
4. In script mode, `EXPLAIN OUTPUT` cannot name the columns of the last statement. The driver then reads each column as a
   string, as it does for any statement that EXPLAIN OUTPUT does not take (D219 item 4).
5. A `CAST` of a string to `JSON` gives a JSON string, and `JSON_PARSE` gives the value
   (`TestIntegrationRoundTrip/JSON`).
6. The Jakarta endpoint resets many connections (41 in one recording). The driver sends again a request that made no
   connection, and a GET that a reset stopped. It never sends a POST again after the connection was made, because the
   statement can have run (D8), so a reset on a POST is an error that the caller sees.
7. Each query is two instances, EXPLAIN OUTPUT and the statement, and a job takes from 0.2 to 3 seconds
   (`TestIntegrationSmoke` took about 2 seconds).
8. The time zone of a `DATETIME` and a `TIMESTAMP` needs the zone database of the machine. A machine that has none gets
   an error for such a column (D219 item 2).

## Second opinions

Gemini (`gemini-3.8-flash`) and DeepSeek (`deepseek-v4-flash`) gave leads on 2026-10-29, as step 7 asks. DeepSeek
timed out twice and answered at the third try. Each lead was sent to the project, and the entry of `features.json`
names its file (item 15 of the manifest).

Right:

- Gemini: `EXPLAIN OUTPUT <query>;` answers the name and the type of each column, with no table ("lead: explain
  output of every type"). This is the way to type a result. DeepSeek named a temporary table with `CREATE TABLE ...
  AS`, which also works but costs a table, and `EXPLAIN OUTPUT` needs none.
- Both: `LIMIT <offset>, <count>` with `ORDER BY` and `ROW_NUMBER()` slices read the rows after the first
  10,000 ("lead: a limit with an offset", "lead: row_number slices").
- Gemini: the setting `odps.sql.select.output.format` with `needHeader` changes the result to a text table with
  borders, which is no gain ("lead: set the output format with a header").
- DeepSeek: `GET <instance>?taskname=<name>&detail` and `&summary` answer JSON about the job, with no column
  types in the first lines read ("lead: the task detail", "lead: the task summary").

Wrong:

- Both: a query parameter or a header on `?result` (`format=json`, `rowstart`, `rowcount`, `offset`, `limit`,
  `Accept: application/json`) changes nothing, and the answer is the same CSV ("lead: the result as json", "lead: the
  result with a row range").
- Both: the settings `odps.sql.read.table.max.row`, `odps.sql.select.auto.limit` and `odps.sql.result.csv.null` are
  accepted and change nothing: the result still holds 10,000 rows and the NULL is still `"\N"` ("lead: a hint for
  the row limit of a result", "lead: a CSV null setting").
- DeepSeek: `TYPEOF` is not a function ("lead: typeof for the types of a result"). Gemini and DeepSeek: a
  server side parameter. `DECLARE @p BIGINT = 1` is a parse error ("lead: the declare of a variable"), and no
  `Parameters` element exists.
- Gemini: a quoted `"\N"` is a string. A NULL is quoted too, and a string of a backslash and N has another form.

Review of the type table (step 8a), by the same two models on 2026-10-29, for a table that used the quoting rule
(before the decision above):

- Both said that the quoting rule violates the rule that the type of the column names the Go type, and that
  the driver must fetch the types. The decision above does this.
- Both said that `DECIMAL`, `DATE`, `DATETIME`, `TIMESTAMP`, `BINARY` and `INTERVAL` must not be strings. The
  table above gives each its kind.
- Both said that `JSON`, `ARRAY` and `MAP` must decode. `JSON` decodes in the table. An `ARRAY`, a `MAP` and a
  `STRUCT` cannot be parsed back from the text of the server, so the table keeps the text.
- Gemini said to read a quoted `"\N"` as a string. This is wrong, as above.

## Decided by Ken on 2026-10-29

These answers are in the conversation. Step 9 will turn them into decisions.

1. The driver reads a result as CSV only. It does not write the Tunnel and does not use Arrow,
   so it needs no binary encoding and no package. A result has at most 10,000 rows, and the
   server cuts a longer one with no sign. Answer 2 of the second list replaces the rule that
   the driver returns an error.
2. The SDK source is the only reference until `dbsetup` finishes the account. The code of the
   driver is written here and copies no code from the SDK.
3. The driver signs with V4 only, and never with V2. V4 needs the region, so the driver reads it
   from the DSN or from the host of the endpoint.

## Decided by Ken on 2026-10-29, after step 6

1. The driver types a column from its CSV cell. An unquoted cell is an `int64`, a `float64` or a
   `bool`, and a quoted cell is a string. A DECIMAL, DATE, TIMESTAMP and BINARY value arrives as a
   string. The driver makes no extra call to learn types.
2. The server cuts a result at 10,000 rows with no sign. The driver returns those rows with no
   error, and this document records the cap. This replaces the first answer of 2026-10-29 that
   the driver must return an error. Step 9 must say so in the decision.
3. The driver reads the region from the host name `service.<region>.maxcompute.aliyun.com`. A
   `region` key in the DSN overrides it, for a custom host.

4. A text result (`DESC`, `SHOW`, `EXPLAIN`) is one column named `text` with one row, which holds
   the whole text. Decided by Ken on 2026-10-29.
5. The caller sets script mode. The driver never parses a query to count its statements. The
   caller passes `odps.sql.submit.mode=script` through a DSN key or a hint, and the driver sends
   the settings that it receives. Decided by Ken on 2026-10-29.
6. The DSN is `maxcompute://<id>:<secret>@service.<region>.maxcompute.aliyun.com/<project>`.
   The key id and the secret are the user and the password, and `region` is an optional key.
   Accepted by Ken on 2026-10-29.

3. A `DATETIME` and a `TIMESTAMP` cell is a `time.Time` in the time zone of the project. The zone comes from the
   setting `odps.sql.timezone` if the caller gives it, and from the property of the same name that `GET
   /api/projects/<project>` answers otherwise ("the project": the value is `Asia/Shanghai`). A probe on
   2026-10-29 (not recorded) showed that the text of a `DATETIME` follows the setting: with no setting
   `UNIX_TIMESTAMP(DATETIME'2026-01-01 00:00:00')` was 1767196800, which is UTC+8, and with the setting `UTC` it was
   1767225600. Decided by Ken on 2026-10-29.
4. An `ARRAY`, a `MAP` and a `STRUCT` cell is the text of the server, in the kind `other`. Decided by Ken on
   2026-10-29.
5. A DSN key that starts with `odps.` is a setting of every statement, such as `?odps.sql.submit.mode=script`.
   Decided by Ken on 2026-10-29.

## Open questions

These wait for Ken. Do not decide them here.

None.

## Integration tests

The tests that need a project read the DSN from `MAXCOMPUTE_DSN`, and skip when it is empty (D9). `dbrun` cannot start
MaxCompute, so the condition is the one of Snowflake (D183 item 13): a person gives an account. The form is the DSN of
D218, with the access key id and the secret of a key that can create tables:

```bash
export MAXCOMPUTE_DSN='maxcompute://<id>:<secret>@service.<region>.maxcompute.aliyun.com/<project>'
go test -race -count=1 -run Integration ./maxcompute/...
```

The tests make tables with the prefix `dbimp_it_` and drop them. A project that holds such a table from a run that
stopped must drop it first. The run of 2026-10-29 on the project `dbimp` in ap-southeast-5 passed every test: the
round trip of each of the 22 types, the 36 entries of the survey that are not types (the 7 with the verdict no are
statements that the server refuses), and the tests of the refusal of a credential, of the arguments, of the errors, of the
cap, of the time zone, of the deadline and of two queries at once. The whole run takes about ten minutes.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was written on 2026-10-29 from the staged code.
A fact of Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of MaxCompute from the sections above.

### The server

| | Couchbase | MaxCompute |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /api/projects/<project>/instances`, with an XML task that holds the statement and the settings, then `GET <instance>?taskstatus` and `GET <instance>?result` (Requests) |
| Database | The key `query_context` of the body | The project, in the path and in the query `curr_project` (Requests) |
| Language | SQL++, which is close to SQL | MaxCompute SQL, close to Hive SQL. One statement for each request, or a script that the caller turns on (Statements) |
| DDL | In SQL++ | In SQL. A foreign key, a unique constraint and a general index are refused, and UPDATE, DELETE and MERGE work on a transactional table only (Statements, Transactions) |
| Parameters | `?`, `$1` and `$name` | None. The driver writes each argument into the text (Parameters) |
| Framing | One body for the whole result, which does not page | An XML answer that holds the CSV of the rows as character data, at most 10,000 rows, cut with no sign (Responses) |
| Columns | `signature`, before the first row | The first line of the CSV names them, and no answer of the REST API gives a type. The statement `EXPLAIN OUTPUT` gives the types (Responses, Types) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. A repeated name gets a digit in the CSV (Responses) |
| Errors | Can come with HTTP 200, after some rows | A statement that does not parse is HTTP 400 with an XML error. A statement that fails is a task in the state Failed, with the message as its result. A cut result has no marker at its end (Errors) |
| Types | JSON. No date, decimal, UUID or binary | Text in CSV, with the declared type of each column. DATE, DATETIME, TIMESTAMP, DECIMAL, BINARY, INTERVAL and JSON have a Go type (Types) |
| Cancel | The server stops a query when the client leaves | The instance runs on, and `PUT <instance>` with the state Terminated stops it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` does not parse (Transactions) |
| Authentication | Basic | A signature of version 4 on each request, from the access key id and the secret (Requests) |
| Default port | 8093, or 18093 with TLS | 443, with TLS always (The DSN) |

The differences that a caller sees:

- A statement is three calls, and a query is six, because the driver asks for the types first (D219).
- A result has at most 10,000 rows, and nothing says so (D220 item 5).
- A time and a date keep their types, as in Databricks, where Couchbase gives JSON shapes (D135 and D219).
- A transaction has no form (D20 and D220 item 4).
- A caller turns on script mode, and the driver never counts the statements of a query (D220 item 3).

### The driver

| | `couchbase` | `maxcompute` |
| --- | --- | --- |
| Size, without tests, on 2026-10-29 | About 1300 lines in 8 files | About 2600 lines in 12 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `ID`, `Secret`, `Project`, `Region`, `Settings` and `Insecure`. The DSN has the keys `region`, `tls` and each key that starts with `odps.` (D218) |
| Options for one statement | Six `With` options for one statement, and two for `BeginTx`. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithProject`, through `WithOptions` or an argument (D109). `WithParameter` sets a setting of the server. `WithReadonly(true)` fails with `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Written into the text as literals with the parser for placeholders of the root package. A list, a map and NaN fail (`maxcompute/params.go`, D220 item 2) |
| Rows | `dbimp.ObjectRows` from the root package | A reader of the XML and a reader of the CSV, both written here, so that a result streams (`maxcompute/xml.go`, `maxcompute/csv.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType` and `ColumnTypeNullable` | The same, `ColumnTypeLength` for a VARCHAR and a CHAR, and `ColumnTypePrecisionScale` for a DECIMAL. `ColumnTypeNullable` returns `true` and `false` |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says. An ARRAY, a MAP and a STRUCT are the text of the server (D219) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` give an error that wraps `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D20 and D220 item 4) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | When the context ends while the driver polls, the driver sends the state Terminated to the instance, with a limit of 5 seconds (D220 item 7) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Message, RequestID}`, which unwraps to `*dbimp.StatusError` for an HTTP status, and the sentinels `ErrCanceled` and `ErrCut` |
| Authentication | Basic | Signature version 4, which the driver sends only to the host of the DSN. The driver follows no redirect (D221) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, the two sentinels, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a date, a decimal and an interval too, where Couchbase gives JSON shapes (D135 and D219).
- `RowsAffected` always gives an error, where Couchbase gives the count (D178 item 14).
- The driver sends a statement once, and a reset after the connection was made is an error that the caller sees, where
  Couchbase has the same rule for a POST (D8 and D221 item 4).
- The caller sets script mode, and a script that holds two queries that print is refused by the server (D220 item 3).
- A DSN needs a key, a secret and a project, and a host of another form needs the key `region` (D218).
