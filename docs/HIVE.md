# Apache Hive and Apache Impala

This file holds what is known about Apache Hive and Apache Impala, for one driver, the package `hive`
(W56 and D233). The headings are the template of [DRIVER.md](DRIVER.md). A fact is "measured", with the
release and the date, or "not measured", with its source.

The recordings are under `testdata/hive/`. The command `dbimptest/cmd/record` made them on 2026-11-09 from
`testdata/hive/requests.json`, for four releases: `hivehttp-4.2.1`, `hivehttp-4.0.1`, `impalahttp-4.5.3` and
`impalahttp-4.4.1`. Each recording has one file for each Thrift call, and the body of a request or a reply
is base64 of the Thrift bytes. The recorder redacts the password in the call `OpenSession`. A fact below is
from 4.2.1 and 4.5.3 unless it names another release. The two older releases answered the same, except where a
fact says otherwise. The survey of step 5a is `testdata/hive/features.json`.

## Summary

- Products: Hive, a SQL engine over Hadoop storage, and Impala, a SQL engine over the same metastore. Both
  serve the `TCLIService` of HiveServer2, which is Thrift.
- Releases in `dbrun`: Hive 4.2.1 and 4.0.1 as `hivehttp-4.2.1` and `hivehttp-4.0.1`, and Impala 4.5.3 and
  4.4.1 as `impalahttp-4.5.3` and `impalahttp-4.4.1`. They are `Verified`, so CI never starts them. The
  binary entries `hive-*` and `impala-*` stay. The `dbmeta` session added the HTTP entries on 2026-11-09.
  They are staged there and not in a tag.
- R: measured. `dbrun` starts the HTTP entries. Hive listens on container port 10001 with the path
  `cliservice`, and any other path fails. The binary port 10000 refuses HTTP. Impala listens on 28000
  (`hs2_http_port`) and serves every path.
- H: measured. A call is an HTTP POST of a Thrift message (Requests).
- S: the statements are HiveQL and the SQL of Impala. A person types them.
- Binary encoding: Thrift, approved by Ken on 2026-11-09 (D232). The codec is in the root package
  (`thrift.go`), and the recorder uses it.
- Package and registered name: `hive`, with Impala as a flavor (D233).
- Scheme in `dburl`: `hive`, alias `hive2`, generator `GenHive`, and `impala`, generator
  `GenScheme("impala")`.
- Drivers that `usql` uses now: `github.com/beltran/gohive/v2` v2.1.0 for Hive, and
  `github.com/sclgo/impala-go` for Impala.

## Requests

Measured on both flavors. A call is `POST /cliservice` with `Content-Type: application/x-thrift`,
`Accept: application/x-thrift` and a Basic `Authorization` header. The body is the strict Thrift binary
protocol: the four bytes `80 01 00 01` (the version and the message type CALL), the name of the call as a
string, the sequence number as an i32, and a struct whose field 1 is the request struct. The reply has the
same framing with the message type REPLY (`80 01 00 02`), and its field 0 is the result struct. An exception
has the type 3 and a struct with the message (field 1) and a type (field 2).

Both servers accept any user and any password (`dbmeta`, measured). Hive answers HTTP 401 when the request
has no Basic header, with the text "Authorization header received from the client is empty". Impala accepts a
request with no header. Hive sets the cookie `hive.server2.auth`, which the driver does not need. Impala
answers with `Server: Thrift/0.16.0`.

The calls that ran: `OpenSession`, `ExecuteStatement` (with `runAsync` true), `GetOperationStatus`,
`GetResultSetMetadata`, `FetchResults`, `CancelOperation`, `CloseOperation` and `CloseSession`, and the
catalog calls under Second opinions. `OpenSession` with `client_protocol` 9 (V10) got `serverProtocolVersion` 9
from Hive and 5 (V6) from Impala. A session handle and an operation handle are structs of a 16 byte `guid`
and a 16 byte `secret`. An operation handle also holds `operationType` (field 2) and `hasResultSet` (field
3), and the server refuses a handle that lacks `operationType`.

## The DSN

Decided in D234. The URL of `dburl` for `hive` keeps its scheme and takes `auth`, `service`, `transport` and
`httpPath` as query keys of `gohive`. The `dbmeta` DSN is
`hive://hive:<password>@127.0.0.1:<port>/default?auth=PLAIN&transport=http&httpPath=cliservice`, and the
Impala one has no path. The driver here is HTTP only. The driver must use no form that comes from an
earlier driver (D27). See the open questions.

## Responses

Measured on both flavors. A call that starts a statement answers with a `TStatus` (field 1) and an operation
handle (field 2). `GetOperationStatus` gives the state (field 2): 1 is RUNNING, 2 is FINISHED, 3 is CANCELED,
5 is ERROR and 7 is PENDING. Impala shows 7 first and Hive shows 1 for a statement that takes time. The
result schema comes from `GetResultSetMetadata`, which holds for each column its name, a type entry (a list
that holds the type id and its qualifiers) and its position. The names and their order arrive before the first
row.

A result is columnar. `FetchResults` returns `TRowSet` with `startRowOffset` (field 1), an empty row list
(field 2) and `columns` (field 3). Each column is a union with one list of values and a bitmap of NULLs. The
union member says the kind of list: 1 bool, 2 byte, 3 i16, 4 i32, 5 i64, 6 double, 7 string and 8 binary. A
NULL is a set bit of the bitmap, and the list holds a placeholder (0 for a number, an empty string for a
string) in its place. The bitmap is one byte `00` on Hive when no value is NULL, and empty on Impala.

Paging is measured with 1000 rows and a fetch of 100 rows. Both flavors returned ten pages of 100 rows and then
an empty page. Hive reports `hasMoreRows` false on every page, and `startRowOffset` 0 on every page, so the
driver must read until a page is empty. Impala reports `hasMoreRows` true until the last full page, and its
`startRowOffset` counts the rows that it returned. A fetch of 5000 rows returned all 1000 rows in one page on
both. A fetch of one row returned one row for each call. The default fetch size that `OpenSession` reports is
`hive.server2.thrift.resultset.default.fetch.size` 1000 on Hive. No cap on the size of a page was found.

A statement that has no result set, such as `create table` or `use`, has `hasResultSet` false in its operation
handle. Hive answers `FetchResults` for it with a `TStatus` of 3 and the message `Cannot invoke
"java.util.List.iterator()" because "fieldSchemas" is null`. Impala answers with an empty page. The driver must
read `hasResultSet` and never fetch when it is false.

A response is never compressed in the recordings, and neither server redirected a request. The recording
holds no response with HTTP 429. HTTP 500 is the answer of Hive and Impala to a `GetInfo` call for an info type
that they do not have (Second opinions).

## Types

Measured on both flavors. The table lists the type id of `TTypeId` for each type that ran, the union member that
holds its values, and a value as measured. The table of step 8a is below.

| Type | Type id | Column list | Value as measured |
| --- | --- | --- | --- |
| BOOLEAN | 0 | bool | `true` |
| TINYINT | 1 | byte | 127 and -128 |
| SMALLINT | 2 | i16 | 32767 |
| INT | 3 | i32 | 2147483647 |
| BIGINT | 4 | i64 | 9223372036854775807 and -9223372036854775808 |
| FLOAT | 5 | double | 1.1 for `cast(1.1 as float)` (a float arrives as a double) |
| DOUBLE | 6 | double | 1e+308 |
| STRING | 7 | string | `x`, and `héllo 日本` and an empty string |
| TIMESTAMP | 8 | string | `2026-06-30 23:59:59.999999999` |
| BINARY | 9 | binary | the bytes `00 ff 0a ff` and an empty value (Impala 4.5 has BINARY) |
| ARRAY | 10 | string | `[[1,2],[3]]` as JSON text (Hive) |
| MAP | 11 | string | `{"k":[1,2]}` as JSON text (Hive) |
| STRUCT | 12 | string | `{"x":1,"y":"z"}` as JSON text (Hive) |
| UNIONTYPE | 13 | string | `{0:1}` (Hive) |
| DECIMAL | 15 | string | `1.50` for `decimal(10,2)`, `-1.0000000000` for `decimal(38,10)`, `100` for `decimal(10,0)` |
| NULL | 16 | string | a NULL, for `select null` (Hive) |
| DATE | 17 | string | `0001-01-01` and `9999-12-31` |
| VARCHAR | 18 | string | `abc`, with `characterMaximumLength` 5 in the qualifiers |
| CHAR | 19 | string | `ab  ` for `char(4)`, padded, with `characterMaximumLength` 4 |
| INTERVAL_YEAR_MONTH | 20 | string | `1-2` (Hive) |
| INTERVAL_DAY_TIME | 21 | string | `1 02:03:04.500000000` (Hive) |
| TIMESTAMPLOCALTZ | 22 | string | `2026-01-02 03:04:05.0 GMT` on 4.2.1 and `2026-01-02 03:04:05.0 Etc/UTC` on 4.0.1 (Hive) |

The precision and the scale of a decimal are in the type qualifiers of the metadata (`precision` and `scale`).
Hive keeps the scale in the text, so `1.50` stays `1.50`. A value that has more digits than its precision is a
NULL on Hive (the cast of 39 digits to `decimal(38,10)`). Impala types the literal `3` as TINYINT, and Hive types
it as INT. A time zone type of Impala, such as `timestamp with time zone`, is refused. Hive refuses nothing of
the list above. ARRAY, MAP and STRUCT were run on Hive only. Impala has no literal for them, so the run of
their columns in a table is not measured.

NULL, missing and empty values: a NULL has the bit set in the bitmap of its column. An empty string and an empty
binary value are values, and their bit is clear. Hive types `select null` as NULL (16).

### The type table of the driver

The tests generate this table from the code (step 10).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INT | integer | `int64` | `int64` | `INT` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| FLOAT | float | `float64, as the server sends it` | `float64` | `FLOAT` | yes |
| DOUBLE | float | `float64` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `*apd.Decimal, with the scale of its column` | `*apd.Decimal` | `DECIMAL` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| CHAR | string | `string, with its padding` | `string` | `CHAR` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| BINARY | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| hive ARRAY | array | `[]any, read from the JSON text of the server` | `[]interface {}` | `ARRAY` | yes |
| hive MAP | map | `map[string]any, read from the JSON text of the server` | `map[string]interface {}` | `MAP` | yes |
| hive STRUCT | map | `map[string]any, read from the JSON text of the server` | `map[string]interface {}` | `STRUCT` | yes |
| hive INTERVAL_YEAR_MONTH | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL_YEAR_MONTH` | yes |
| hive INTERVAL_DAY_TIME | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL_DAY_TIME` | yes |
| hive UNIONTYPE | other | `string, the text of the server` | `string` | `UNIONTYPE` | yes |
| hive TIMESTAMP WITH LOCAL TIME ZONE | timestamp | `time.Time` | `time.Time` | `TIMESTAMP WITH LOCAL TIME ZONE` | yes |
| hive NULL | null | `nil` | `interface {}` | `NULL` | yes |
<!-- /dbimp:types -->

### The mapping of step 8a

Ken accepted this mapping on 2026-11-09 (D234). The kinds are those of [TYPES.md](TYPES.md).

| Type | Kind | Go type |
| --- | --- | --- |
| BOOLEAN | boolean | `bool` |
| TINYINT, SMALLINT, INT, BIGINT | integer | `int64` |
| FLOAT, DOUBLE | float | `float64` |
| DECIMAL | decimal | `*apd.Decimal`, with the scale of its column (D228) |
| STRING, VARCHAR, CHAR | string | `string` |
| BINARY | binary | `[]byte` |
| DATE | date | `dbimp.Date` |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime` |
| TIMESTAMPLOCALTZ | timestamp | `time.Time` |
| INTERVAL_YEAR_MONTH, INTERVAL_DAY_TIME | interval | `dbimp.Interval` |
| ARRAY | array | `[]any`, read from the JSON text |
| MAP | map | `map[string]any`, read from the JSON text |
| STRUCT | map | `map[string]any`, read from the JSON text |
| UNIONTYPE | other | the text of the server |
| NULL | null | `nil` |

The three points were: ARRAY, MAP and STRUCT are decoded from the JSON text, STRUCT is a map and loses the order of its
fields, and a FLOAT is a `float64` as the server sends it. Ken accepted them (D234).

## Parameters

Measured. Neither flavor binds a parameter: `select ?` and `select :a` both fail at `ExecuteStatement`.
`TExecuteStatementReq` has no field for parameters. Hive substitutes a variable of its own, as in `set
hivevar:v=1` and then `select ${hivevar:v}`, which is not a binding. The driver would use the escaper of the
root package (D34).

## Transactions

Measured. Neither flavor has a statement for a transaction over this interface. Hive 4.2.1 answers
`START_TRANSACTION is not supported yet`, `COMMIT is not supported yet` and `ROLLBACK is not supported yet`.
Impala refuses `begin`, `commit` and `rollback`. Hive 4.2.1 does run `update`, `delete` and `merge` on a transactional
table, and Impala on an Iceberg table of version 2, and each statement is its own unit. `BeginTx` returns
`dbimp.ErrNotSupported` (D20).

## Errors

Measured. A statement that does not compile fails at `ExecuteStatement` with HTTP 200 and a `TStatus` whose
`statusCode` is 3 (ERROR_STATUS). Hive 4.2.1 gave `sqlState` `42000`, `errorCode` 40000 and the message `Error
while compiling statement: FAILED: ParseException line 1:0 ...` for a syntax error, and `42S02`, `errorCode`
10001 and `SemanticException [Error 10001]: Line 1:17 Table not found 'nothing'` for a table that is not
there. Its `infoMessages` hold `Server-side error; please check HS2 logs.`. Impala 4.5.3 gave `sqlState`
`HY000`, no `errorCode`, and a message of several lines that starts `Query ... failed:` and then names the
exception, as in `AnalysisException: Could not resolve table reference: 'nothing'`.

A statement can compile and fail while it runs. Hive gave `GetOperationStatus` with the state 5, `errorCode` 1
and the message `ASSERT_TRUE(): assertion failed` for `select assert_true(false)`. This arrives before any
fetch, so no error comes after some rows in these recordings. An error after some rows is not measured.

A call with an operation handle that the server never issued gets `TStatus` 3 with `Invalid OperationHandle`
on Hive. A call that lacks a required field is a Thrift exception, such as `Required field 'maxRows' is unset`.
A call with a name that the server does not have is a Thrift exception, `Invalid method name: 'GetQueryId'` on
Impala. Hive answers HTTP 401 for a request with no credential, and Impala accepts it. Both accept a wrong
password. A rate limit is not measured, and no call fails in a way that proves that the statement never reached
the server, apart from the HTTP 401 and a refused connection.

## Cancellation and timeouts

Measured. `CancelOperation` works while a statement runs. A statement that counted 10 to the 11th power rows
ended on Hive in the state 3 (CANCELED) after the call. On Impala, `select sleep(30000)` was FINISHED at once
and its first `FetchResults` blocked. `CancelOperation` from a second request ended that fetch with an error
status. The query that counted 10 to the 11th power rows ended on Impala in the state 5. Closing the session or
the operation ends the work, and the driver sends `CloseOperation` and `CloseSession`. A timeout of the client is
not measured. `TExecuteStatementReq` has a field `queryTimeout`, which is not measured.

## Statements

Measured. One statement for each `ExecuteStatement`. A second statement after a semicolon is a parse error on
both. Hive refuses a semicolon at the end of a statement, and Impala accepts it. A line comment and a block
comment before or after a statement work on both. An empty statement is a call that was not sent in the
recording, so it is not measured.

## Principals

Measured by `dbmeta` and by the recordings. Both images run with no authentication, so any user and any password
is accepted and no ordinary user exists. The recordings are of the user `hive` and the user `impala`, with the
password that `dbrun` printed. The manifest names no ordinary user.

## Flavors

Ken decided on 2026-11-09 that the package is `hive` and that Impala is a flavor of it (D233). It registers one
name, `hive`, and the scheme `impala` of `dburl` reaches it with its own `Dialect`. Each flavor has its own
container entry, its own measurements and its own verdicts in `features.json`, where a difference has a name
that starts with `hive` or `impala`.

The call `GetInfo` tells the flavors apart without a statement. With the info type 13 (CLI_SERVER_NAME) Hive
answers `Hive` and Impala answers `Impala`. With 17 (CLI_DBMS_NAME) Hive answers `Apache Hive` and Impala answers
`Impala`. With 18 (CLI_DBMS_VER) Hive answers `4.2.1` and Impala answers `4.5.3-RELEASE`. The info types 0, 29 and
43 are refused: Hive gives HTTP 500, and Impala gives `TStatus` 3 with `HYC00` and `Unsupported operation`. The
statement `select version()` answers `4.2.1 r0f4761a83b40a279560359fa611a65774e42324b` on Hive and a text of
several lines on Impala that starts `impalad version 4.5.3-RELEASE`.

The differences that are measured:

- The protocol version that the server returns: 9 on Hive and 5 on Impala.
- The first state of an operation (7 on Impala), `hasMoreRows` and `startRowOffset`, the bitmap, and the fetch
  of a statement with no result set (Responses).
- The form of an error (Errors), and the call `GetQueryId`, which Hive has and Impala refuses.
- Update, delete and merge: Hive runs them on a transactional ORC table and Impala on an Iceberg table, and both
  refuse them on a plain table. Impala refuses `create materialized view`, and a primary key on a table that is not
  Kudu. Hive accepts a materialized view, a primary key, a
  foreign key, a unique constraint and a default value, each declared `disable novalidate`.
- Types: Hive types an integer literal as INT and Impala as TINYINT. BINARY runs on both. ARRAY, MAP, STRUCT,
  UNIONTYPE and the intervals ran on Hive only.
- Hive 4.0.1 differs from 4.2.1 in one group: `create table ... tblproperties ('transactional'='true')` and
  every statement that depends on it fail with `SemanticException [Error 10265]: This command is not allowed on
  an ACID table ... with a non ACID transaction manager`. So `update`, `delete`, `merge` and a materialized view
  do not run on 4.0.1 as `dbrun` starts it. The other 112 statements answered alike on both Hive releases.
- Impala 4.4.1 refuses `merge into` with a parse error, and 4.5.3 runs it on an Iceberg table. The other 111
  statements answered alike on both Impala releases.

## Interfaces

The tests generate this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. Connect opens a session. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials and the session. |
| `driver.SessionResetter` | no | A connection holds a session, but a statement changes no state of it that database/sql does not end, because WithDatabase fails and a transaction does not exist. |
| `driver.Validator` | yes | A connection whose session the server no longer knows is not valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64 and the civil types of the root package, which the driver writes as literals itself (D235). |
| `driver.QueryerContext` | yes | A query starts an operation, waits for it, and reads its rows page by page (D235). |
| `driver.ExecerContext` | yes | Exec runs the statement to its end and reads no row. Hive gives the count of rows that it changed, and Impala does not. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments written as literals, each time, because the server binds none. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because neither flavor has a transaction over this interface (D235). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned, so a decimal keeps its scale. |
| `driver.RowsNextResultSet` | no | A statement has one result. |
| `driver.RowsColumnTypeScanType` | yes | The type id of each column gives the Go type (D234). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type id of each column gives its name in upper case. |
| `driver.RowsColumnTypeLength` | yes | A VARCHAR and a CHAR name their length in the qualifiers of the type. |
| `driver.RowsColumnTypeNullable` | yes | The metadata does not name NOT NULL, so each column can hold NULL. |
| `driver.RowsColumnTypePrecisionScale` | yes | A DECIMAL names its precision and its scale in the qualifiers of the type. |
<!-- /dbimp:interfaces -->

## Faults

Not measured, except the first. `gohive` panics with `Unrecognized auth` when `auth` is missing or unknown (source:
`dbmeta` `container/hive.go`). The driver here has no such key and never panics. The other faults of `gohive` and `impala-go` were not
measured.

## Second opinions

Asked on 2026-11-09: Gemini (`gemini-3.8-flash`) and DeepSeek (`deepseek-v4-flash`). Each lead and what the
server said:

- The catalog calls `GetInfo`, `GetTypeInfo`, `GetCatalogs`, `GetSchemas`, `GetTables`, `GetTableTypes`,
  `GetColumns`, `GetFunctions`, `GetPrimaryKeys` and `GetCrossReference`: both models named them. All ten
  answered on both flavors, and each starts an operation that `FetchResults` reads, except `GetInfo`. Hive
  returned 23 rows for `GetTypeInfo`, Impala 17. `GetCatalogs` returned no row on either. The column names of
  Impala hold two faults of its own: `TABLE_MD` where Hive has `TABLE_SCHEM` in `GetColumns`, and `KEQ_SEQ` where
  Hive has `KEY_SEQ` in `GetPrimaryKeys`.
- `GetQueryId`: both models named it with a question mark for Impala. Hive returned the query id. Impala raised
  an exception, `Invalid method name`.
- The log of an operation through `FetchResults` with `fetchType` 1: Gemini named it. Hive returned the lines of its
  log. Impala returned one column with a number, which holds no log text.
- Delegation tokens (`GetDelegationToken`, `CancelDelegationToken`, `RenewDelegationToken`): both models named them.
  Not measured, because the recordings use no Kerberos.
- Progress through `GetOperationStatus`: DeepSeek named it. Not measured. The status reply of Hive holds fields 7 to
  10 that were not read.
- Parameters: both models said that neither flavor binds them over this interface. Measured, and right.
- Impala materialized view: DeepSeek said yes and Gemini said no. Measured on 4.5.3 and 4.4.1: refused. Gemini was
  right.
- Impala update and delete: Gemini said Kudu and Iceberg v2. DeepSeek was unsure about Iceberg. Measured on 4.5.3
  and 4.4.1: they work on an Iceberg table of version 2, and fail on a plain table.
- Hive transactions: both models said yes. Measured: the statements `start transaction`, `commit` and `rollback`
  fail on 4.2.1 with "is not supported yet". The models were wrong.
- Hive constraints: both models said that Hive keeps a primary key, a foreign key and not null. Measured: they
  are accepted with `disable novalidate`.

## Open questions

1. None. Ken accepted D235 on 2026-11-09.
2. The hand built call of the `dbmeta` session got no answer from Impala, and the call here did. The cause is not
   found.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was written on 2026-11-09 from the code.
A fact of Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of this driver from the sections above.

### The server

| | Couchbase | Hive and Impala |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /cliservice` of a Thrift message, one call for each step (Requests) |
| Database | The key `query_context` of the body | The database of the session, set by `USE` (Statements) |
| Language | SQL++, which is close to SQL | HiveQL, and the SQL of Impala |
| DDL | In SQL++ | In SQL. A statement with no result set has `hasResultSet` false (Responses) |
| Parameters | `?`, `$1` and `$name` | None. The driver writes literals (Parameters) |
| Framing | One body for the whole result, which does not page | One reply for each call, and a result in pages of columns (Responses) |
| Columns | `signature`, before the first row | `GetResultSetMetadata`, before the first page |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 with a `TStatus` of 3 for a statement that does not compile, and the state ERROR of the operation for one that fails while it runs. Never after some rows in the recordings (Errors) |
| Types | JSON. No date, decimal, UUID or binary | Typed columns. A decimal, a date and a timestamp arrive as text, and an array, a map and a struct as JSON text (Types) |
| Cancel | The server stops a query when the client leaves | `CancelOperation` stops a statement, and closing the operation ends it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None over this interface (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic. Both servers accept any user and any password, and Hive answers HTTP 401 to a request with no credential |
| Default port | 8093, or 18093 with TLS | 10001 on Hive, and 28000 on Impala as `dbrun` publishes it |

The differences that a caller sees:

- A connection is a session of the server, and a statement is an operation that the driver waits for.
- The driver reads a result page by page, and asks for the next page when `Rows.Next` needs it (D235).
- Neither flavor binds a parameter, so the driver writes each argument into the text of the statement (D235).
- `BeginTx` returns `dbimp.ErrNotSupported` (D235).
- Hive says how many rows a statement changed, and Impala does not.

### The driver

| | `couchbase` | `hive` |
| --- | --- | --- |
| Size, without tests, on 2026-11-09 | About 1300 lines in 8 files | About 2300 lines in 10 files, and the Thrift codec in the root package |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Database`, `HTTPPath` |
| Options for one statement | Six `With` options, through `WithOptions` or an argument (D40, D46 and D109) | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithFetchSize`, through `WithOptions` or an argument (D109). `WithReadonly(true)` and `WithDatabase` give `dbimp.ErrNotSupported`. `WithParameter` sets a key of `confOverlay` |
| Arguments | Sent to the server as `args` and `$name` | Written into the text of the statement as literals, with the parser for placeholders of the root package (D34 and D235) |
| Rows | `dbimp.ObjectRows` from the root package | A page of columns, decoded one row at a time (`rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, from the type id, and `ColumnTypePrecisionScale` and `ColumnTypeLength` from the qualifiers (D234) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type id, as D234 decides |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from Hive, and `dbimp.ErrNotSupported` from Impala. `LastInsertId` is `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D235) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | `IsValid` only. A statement changes no state of the session that `database/sql` does not end |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The request carries the context. When it ends, the driver sends `CancelOperation` and `CloseOperation` with a context of its own (D235 and D236) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{SQLState, Code, Message, Info, HTTPStatus}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx |
| Authentication | Basic | Basic, and the user `anonymous` for a DSN with no user |
| Other exports | The `With` options and `Option` | The `With` options and `Option` |

The differences that a caller sees:

- A value has the Go type of its type id, and a decimal keeps the scale of its column (D228 and D234).
- A connection and its rows hold a context (D236), which the other drivers of its kind also do.
- The driver needs no JSON, and decodes an array, a map and a struct from the text of the server.

Still to measure: an error after some rows, a session that expires, a timeout of the client, `queryTimeout`,
TLS, the delegation tokens, the progress fields, ARRAY, MAP and STRUCT columns on Impala, and a cookie of
Hive in a replay.
