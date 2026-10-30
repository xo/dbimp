# Tablestore

This file holds what is known about Alibaba Cloud Tablestore (OTS), for the driver `tablestore`
(W48). The headings are the template of [DRIVER.md](DRIVER.md). A fact is "recorded", with the
name of its request in quotes, or "not measured", with its source.

A recorded fact has its request in `testdata/tablestore/requests.json` under that name, and the
exchange in the file that `testdata/tablestore/manifest.json` names. All recordings are of the
instance `dbmeta` in ap-southeast-5 (Jakarta) on 2026-10-29, as one principal, the key
`dbimp-alicloud` with the policy `AliyunOTSFullAccess`. The bodies of the requests are protobuf
that a script of this repository writes by hand, and the recorder keeps each body as base64.

## Summary

- Product: Tablestore, a hosted wide column store with a SQL query API. The name in `dbrun` is
  `tablestore`. The Go package and the registered name are `tablestore`, decided by Ken on
  2026-10-29. `dburl` has the scheme `tablestore` and the alias `ots`.
- R: a hosted service, so `dbrun` cannot start it. The condition is the one of Snowflake.
- H: the API is HTTPS, but a request and a response are protobuf. The rows of a SQL answer are
  FlatBuffers. Ken approved on 2026-10-29 that the driver encodes and decodes both in this
  repository, with no package (D13).
- S: SQL works only over a mapping table, which is a name for a Tablestore table. A person types it.
- The older driver that `usql` uses is `aliyun-tablestore-go-sql-driver` of 2022-04-18.

## Requests

A call is `POST https://<instance>.<region>.ots.aliyuncs.com/<Action>` with a protobuf body
("list tables", "select 1"). The SQL action is `SQLQuery`, and its message holds the text of the
statement (field 1), the payload version (field 2) and `sql_version` (field 3, the value 1). The
request is signed in the headers `x-ots-date`, `x-ots-apiversion` (`2015-12-31`),
`x-ots-accesskeyid`, `x-ots-instancename`, `x-ots-contentmd5` and `x-ots-signature`, which is the
base64 HMAC with SHA-1 of the path, the method and the sorted `x-ots-` headers, under the secret
("select 1"). The server accepted the date as `2026-10-29T07:40:52.000Z`. A signature with the
version 4 form (`x-ots-signaturev4`) is not measured.

## The DSN

`tablestore://<id>:<secret>@<instance>.<region>.ots.aliyuncs.com/<instance>`, decided by Ken on
2026-10-29 and staged in `dburl` v0.54.0. The user is the access key id, the password is the
secret and the path is the instance. `dburl` adds the suffix `.ots.aliyuncs.com` to a host that
is `<instance>.<region>`. The driver reads `tls` as in D50.

## Responses

The answer to `SQLQuery` is a protobuf message with the rows in field 2, the payload version in
field 3, the statement type in field 4, the affected rows in field 6, and the consumed capacity
in field 1. With the payload version 2 (FlatBuffers) the rows hold the columns with their names
and types before the first row ("select 1"). With version 1, or with no version, the server sends
a plain buffer (a third binary form, `0x75` header) ("select 1 with the plain buffer payload",
"select 1 with no payload version"). The driver uses version 2. A statement type 7, 8 and 9 is an
insert, an update and a delete ("insert", "update", "delete"). The answer has no paging in 2600
rows ("every row of 2600"): the whole result of 74 KB came in one answer. A larger result is not
measured, and the message holds `next_search_token` for it.

## Types

A mapping column has one of these types, which the server accepted: BIGINT, DOUBLE, BOOL,
BOOLEAN (the same as BOOL), MEDIUMTEXT, MEDIUMBLOB, DATETIME, DATE and TIMESTAMP ("mapping column
of type BIGINT" and the others of item 3). A key can also be VARCHAR or VARBINARY, and a column
cannot ("mapping column of type VARCHAR(100)"). The server refuses INT, INTEGER, FLOAT, TEXT, BLOB,
BINARY, DECIMAL, JSON, STRING, KEYWORD, GEO_POINT, VECTOR, ARRAY and NESTED. `DESC` names the type
of each mapping column as `bigint(20)`, `double`, `tinyint(1)`, `mediumtext`, `mediumblob`,
`datetime`, `date` and `timestamp`.

The stored value of a DATETIME, DATE and TIMESTAMP column is an integer. A row with an integer
there reads, and a row with a string, a binary or a double there fails with `Field type mismatch`
("select the dates as int", "select the dates as string", "select the dates as binary", "select the
dates as double"). The FlatBuffers rows carry the type of each column. A value that is missing in a
row is a NULL ("select only the row with no columns"). A table stores no NaN and no Infinity ("put
a row with a NaN", "put a row with an infinity"). The step 8a table decides the Go type of each.
The literal NULL in a query has no type, and the server refuses it ("type extra: the types of
literals").

The mapping of step 8a is below. Ken reviewed it on 2026-10-29 except the row of `TIMESTAMP`, which step 14a settles. `TestTables` will make the same table from
the code at step 10. The wire types are the types of a FlatBuffers column (read in the SDK, not yet decoded by this
repository).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| DOUBLE | float | `float64` | `float64` | `DOUBLE` | yes |
| BOOL | boolean | `bool` | `bool` | `BOOL` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOL` | yes |
| MEDIUMTEXT | string | `string` | `string` | `MEDIUMTEXT` | yes |
| MEDIUMBLOB | binary | `[]byte` | `[]uint8` | `MEDIUMBLOB` | yes |
| DATETIME | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `DATETIME` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIMESTAMP | timestamp | `time.Time` | `time.Time` | `TIMESTAMP` | yes |
<!-- /dbimp:types -->

The notes:

- A `DATETIME` and a `TIMESTAMP` column hold an instant, and the server sends it as a count of microseconds since the
  Unix epoch. The server reads the text of a time in its own zone, which is Asia/Shanghai: the statement
  `INSERT ... VALUES ('2026-10-29 01:02:03')` stores 1793206923000000, which is 2026-10-28 17:02:03 UTC (recorded:
  "select the dates as int" holds the wire form, and the integration test `TestIntegrationFeatures/now_function`
  holds the zone). The offset was +8:05:43 before 1901, which the refusal of the date 1000-01-01 shows. The driver
  reads a `DATETIME` as the wall clock of that zone, so that the text that a caller writes is the text that it
  reads, and it gives a `TIMESTAMP` as the instant in UTC. It writes a `time.Time` argument as the wall clock of
  that zone. Ken decided the Go type of `DATETIME`. Whether another instance or region has another zone is an open
  question.
- A `DATE` column holds a count of seconds since the epoch, at 00:00 UTC of the day (the integration test
  `TestIntegrationRoundTrip/DATE` holds it). The first date that the server keeps is 1000-01-02, and the last is
  9999-12-31: the check compares the local midnight of the date, in the zone of the server, with the instant
  1000-01-01 00:00 UTC, and it refuses 1000-01-01 (measured in the live run of 2026-10-29, not recorded). A
  `TIMESTAMP` takes no time before 1970-01-01 00:00:01 UTC (the refusal of '1969-12-31 23:00:00'). The server keeps whole seconds
  and drops a fraction that the text holds.
- The wire form of a `TIMESTAMP` column is a column of the data type `COMPLEX` with the logic type 4 and the
  encode type `LONG` (recorded: "select the dates as int"). The SDK knows the logic types 0 to 3 only. The driver reads
  the count as microseconds, like a `DATETIME`, and the round trip of whole seconds in `TestIntegrationRoundTrip/TIMESTAMP`
  gives the same instant back. The server drops a fraction, so no test shows the unit below a second.
- A key of `VARCHAR` or `VARBINARY` reads as `MEDIUMTEXT` and `MEDIUMBLOB`.
- A `TIME` column exists in the SDK (logic type 3), and the mapping accepts no `TIME` column, so the table has no row
  for it. The driver returns an error that wraps `dbimp.ErrNotSupported` for a column of a type that it does not know.

## Parameters

None. A `?` is refused ("a question mark parameter"), so the driver uses the parser for
placeholders in the root package (D34).

## Transactions

None in SQL. `BEGIN`, `START TRANSACTION`, `COMMIT`, `ROLLBACK` and `SET AUTOCOMMIT` are refused ("a transaction",
"lead: start transaction"), and `REPLACE` and `ON DUPLICATE KEY UPDATE` report that the table does not allow an
explicit transaction ("replace"). `BeginTx` returns `dbimp.ErrNotSupported` (D20).

## Errors

A failure is HTTP 400 with a protobuf message of a code and a text, for example
`OTSParameterInvalid` for a missing table, a syntax error or an unsupported function ("a missing
table", "a syntax error", "a missing column"). A wrong secret is HTTP 403 `OTSAuthFailed` with
"Signature mismatch." ("a wrong secret"). A request with no credentials is HTTP 400 with an XML
body of `OTSUnsupportOperation` ("no credentials"). A body that is not protobuf is HTTP 400
`OTSParameterInvalid` ("a protobuf body that is not valid"). A duplicate key on an insert is HTTP
400 "Duplicate entry for key 'PRIMARY'" ("insert with a null and a duplicate key").

## Cancellation and timeouts

The API has no call to cancel a statement (the manifest names item 7 as absent). The driver
stops by closing the request.

## Statements

A statement is one `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE` (the mapping), `DROP
MAPPING TABLE`, `ALTER TABLE ... ADD COLUMN`, `DESC` or `SHOW`. A mapping needs the table of Tablestore to exist
first, and `CreateTable` of the API makes it (the integration tests do). `DROP MAPPING TABLE` removes the name, and
`DESC` refuses it at once. A `SELECT` can still read the name for a short time after the drop (measured in the
live run of 2026-10-29, not recorded). Two statements in one request are
refused ("two statements"). A comment is accepted ("a line comment", "a block comment").
`EXPLAIN`, `SHOW VARIABLES`, `VERSION()`, `CAST`, `CURDATE`, `CREATE INDEX`, `CREATE VIEW` and
`DROP TABLE` are refused. `SELECT 1 / 0` answers and does not fail ("a division by zero"). A string literal takes a
backslash and a doubled quote as MySQL does: the live run of 2026-10-29 stored and read back `it's \ a "test" é`, which
the driver writes as `'it''s \\ a "test" é'`. The version of the server is `SELECT @@version`, and `SELECT VERSION()` is
refused.

## Principals

One principal, the administrator of `dbimp-alicloud`, because `dbsetup` made one key. A refused
privilege is not measured.

## Flavors

One. The service is hosted, and no release name exists.

## Interfaces

The interface table below is made by `TestTables` from the code.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and the credentials that the driver signs each request with (D222). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping sends ListTable, which costs nothing and checks the endpoint, the signature and the credentials. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each statement is its own request, so there is nothing to reset. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and hands every other value to the default converter, because the driver writes each value as a literal (D224). |
| `driver.QueryerContext` | yes | The statement goes to SQLQuery with its arguments written into its text, because the server binds none (D34 and D224). |
| `driver.ExecerContext` | yes | Exec reads the answer to its end with no decoding, and RowsAffected is the count of the answer, or an error that wraps dbimp.ErrNotSupported when the answer has none (D178). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. The server has no prepared statement. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the server refuses BEGIN and START TRANSACTION (D20 and D224). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so a statement has one result. |
| `driver.RowsColumnTypeScanType` | yes | The rows name the type of each column, and each type has one Go type (D135 and D223). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of the column in upper case, such as BIGINT. A BOOL column is BOOL. |
| `driver.RowsColumnTypeLength` | no | The server sends no length for a column, so the driver reports none. |
| `driver.RowsColumnTypeNullable` | yes | The rows mark a NULL for any column, and the server sends no constraint, so every column can be NULL. |
| `driver.RowsColumnTypePrecisionScale` | no | No type of the mapping has a precision or a scale. |
<!-- /dbimp:interfaces -->

## Faults

The faults of this driver, found while it was written. None is a fault of the server.

1. The driver reads the rows from the stream when a row asks for a value, and it keeps what it read, because FlatBuffers
   refers to any place of the message. It never holds more than the answer, and it holds the part that it read until
   the rows close. A result of 64 MiB costs up to 64 MiB (open question 3).
2. The server does not stop a statement when the client leaves, and the API has no call to cancel one. A statement that
   runs long runs to its end, and the driver returns the error of the context at once (D36 and D224 item 6).
3. The service resets a connection now and then (recorded: the step 6 run needed a second try for some calls). The
   driver never sends a request again after it can have reached the server (D8), so a reset after the request is an
   error to the caller. The integration tests turn off the reuse of connections and send their setup again.
4. `WithTimeout` and `WithReadonly(true)` fail with `dbimp.ErrNotSupported`, because the server has no setting for
   either. The hint `MAX_EXECUTION_TIME` is accepted, and no test showed that it stops a statement.
5. A fraction of a second in a `time.Time`, a `dbimp.LocalDateTime` or a `dbimp.Date` argument never reaches the table,
   because the server keeps whole seconds.
6. A `DATE` before 1000-01-02, and a `TIMESTAMP` before 1970-01-01 00:00:01 UTC, are errors of the server.

## Second opinions

Gemini (`gemini-3.8-flash`) and DeepSeek (`deepseek-v4-flash`) gave leads on 2026-10-29, as step 7
asks. Each lead was sent to the instance, and the entry of `features.json` names its file. A lead is
a lead, and these are the answers of the server (item 15 of the manifest).

Right:

- `SELECT @@version` works and answers `5.7.25-SQLFrontend-b30fece-dirty`. This is the version for
  `usql`. Both models named it, and `SELECT VERSION()` is refused ("lead: the version variable").
- `NOW()`, `CURRENT_TIMESTAMP`, `FROM_UNIXTIME`, `DATE_FORMAT`, `CONCAT` and `i + 0` work ("lead:
  now", "lead: from unixtime", "lead: concat", "lead: an addition of zero"), and `SHOW CREATE TABLE`
  works ("lead: show create table"). `LIMIT 1, 2` works.
- Every `sql_version` of 0, 2, 3 and 4 is accepted, and a search token of no meaning is accepted and
  ignored ("lead: sql_version 3", "lead: a search token of no meaning"). The server did not show what
  `sql_version` changes, and it never sent a `next_search_token`.
- The hint `MAX_EXECUTION_TIME(1000)` is accepted. Whether it stops a long statement is not measured.

Wrong:

- `DATABASE()`, `CURRENT_USER()`, `CURRENT_DATABASE()` and `CONVERT` are refused ("lead: the database
  function"). `SHOW DATABASES`, `START TRANSACTION`, `COMMIT`, `ROLLBACK` and `SET AUTOCOMMIT` are
  unsupported ("lead: start transaction"). `information_schema` is refused as another instance ("lead:
  information schema").
- A time series name of a table (`<table>::multi_model`) is a syntax error on a table that is not a time
  series ("lead: a time series name").
- `JSON_EXTRACT` on a string column that holds no JSON fails with `Invalid JSON text`. The function
  exists, and this is no refusal, so `features.json` has no entry for it.

Review of the type table (step 8a), by the same two models on 2026-10-29:

- Both said that the wire form of `TIMESTAMP` is not known, and that the table must not give it a Go type
  until the FlatBuffers type is decoded. The decoder of this repository showed it: the data type `COMPLEX`, the logic
  type 4 and the encode type `LONG` (recorded: "select the dates as int").
- Both said that a `DATE` count of seconds needs a defined epoch and zone, or a day can be wrong. The SDK reads
  it as seconds since the Unix epoch in UTC (`sql_model.go`), and the round trip of step 14a holds it.
- Both said that a NULL must be `nil` and never a zero value, which hard rule 3 holds already. The
  FlatBuffers rows mark a NULL with a flag for each value, and the driver reads that flag first.
- Gemini said to read a `DATETIME` as UTC. The round trip of step 14a showed that the server reads the text of a
  time in Asia/Shanghai, so the driver reads a `DATETIME` as the wall clock of that zone and not of UTC.
- DeepSeek said to name `DECIMAL`, `JSON` and `INTERVAL`. The mapping accepts none of them, so they stay out.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was written on 2026-10-29 from the code of the
first release. A fact of Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of Tablestore from the sections
above.

### The server

| | Couchbase | Tablestore |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` in JSON | `POST /SQLQuery`, with the text of the statement, the payload version, `sql_version` and a search token, as protobuf (Requests) |
| Database | The key `query_context` of the body | The instance, in the host and in the header `x-ots-instancename`. A mapping names a table of the instance (Statements) |
| Language | SQL++, which is close to SQL | SQL of MySQL 5.7 over a mapping table. One statement for each request. `SELECT`, `INSERT`, `UPDATE` and `DELETE` run, and `REPLACE`, `UPSERT`, `EXPLAIN` and `CAST` do not (Statements) |
| DDL | In SQL++ | A mapping only: `CREATE TABLE` maps an existing table, and the API makes and deletes the table (Statements) |
| Parameters | `?`, `$1` and `$name` | None. A `?` is refused (Parameters) |
| Framing | One JSON body for the whole result, which does not page | One protobuf message that holds the rows as FlatBuffers, in one piece. A result of 2600 rows came whole (Responses) |
| Columns | `signature`, before the first row | In the FlatBuffers rows, with the name and the type of each column (Responses) |
| Errors | Can come with HTTP 200, after some rows | HTTP 400 or 403 with a protobuf code and text, before any row. A request with no credentials gets XML (Errors) |
| Types | JSON. No date, decimal, UUID or binary | `BIGINT`, `DOUBLE`, `BOOL`, `MEDIUMTEXT`, `MEDIUMBLOB`, `DATETIME`, `DATE` and `TIMESTAMP` (Types) |
| Cancel | The server stops a query when the client leaves | The server does not, and the API has no call to cancel (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None in SQL (Transactions) |
| Authentication | Basic | A signature in `x-ots-` headers, with the access key id and the secret (Requests) |
| Default port | 8093, or 18093 with TLS | 443, with TLS by default (The DSN) |

The differences that a caller sees:

- A value keeps its type, a date and a time too, where Couchbase gives JSON shapes (D135 and D223).
- The server binds no argument, so the driver writes each value into the text of the statement (D224 item 1).
- A time is read and written in the zone Asia/Shanghai, which is a fact of the instance (Types).
- A table needs the API to exist before SQL can map it (Statements).

### The driver

| | `couchbase` | `tablestore` |
| --- | --- | --- |
| Size, without tests, on 2026-10-29 | About 1300 lines in 8 files | About 1950 lines in 13 files, of which the encodings of protobuf and FlatBuffers are about 750 |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `Instance`, `User` and `Password`. The DSN has the key `tls` only (D222) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, through `WithOptions` or an argument (D109). `WithParameter` sets `sql_version` and `search_token`. `WithTimeout(d > 0)`, `WithReadonly(true)` and another instance fail with `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Written into the text as literals of MySQL: a quote is doubled, a backslash and a NUL are escaped, a byte slice is `X'hex'`, a time is text in the zone of the server (`tablestore/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own: protobuf, then FlatBuffers, which it reads from the stream when a row asks for a value (`tablestore/result.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType` and `ColumnTypeNullable`, which is always `true` |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says (D223) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from the count of the answer, or `dbimp.ErrNotSupported` when the answer has none. `LastInsertId` always gives `dbimp.ErrNotSupported` (D178) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D224) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same. The server keeps running the statement, and there is no call to stop it (D224 item 6) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Message, RequestID}`, which unwraps to `*dbimp.StatusError` and matches `dbimp.ErrAuthentication` for `OTSAuthFailed` (D197) |
| Authentication | Basic | A signature that the driver writes with the standard library. It follows no redirect, so the signature goes to the host of the DSN only |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, `ErrNoCredentials`, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- The driver has two encodings of its own, which no other driver has (D13 and D222).
- `RowsAffected` gives an error for a statement whose answer holds no count (D178).
- A `DATETIME` is a `dbimp.LocalDateTime`, and a `TIMESTAMP` is a `time.Time` (D223).
- An error after the first row wraps `dbimp.ErrIncomplete`, as in Couchbase (D107).

## Integration tests

The tests of `tablestore/*_integration_test.go` run against a live instance, and skip when `TABLESTORE_DSN` is empty (hard
rule 9). They ran on 2026-10-29 on the instance `dbmeta` in ap-southeast-5, as the one key that `dbsetup` made, with the
race detector, and passed: the CRUD, the schema, the features of `features.json`, the refusals of the types, and the round
trip of each type that the survey marks yes. Each test makes its tables with the prefix `dbimp_it_<run>`, and `TestMain`
deletes the ones that a test left.

```bash
export TABLESTORE_DSN='tablestore://<access key id>:<secret>@<instance>.<region>.ots.aliyuncs.com/<instance>'
go test -race -count=1 ./tablestore/...
```

## Making and deleting a table

SQL cannot make a table. `CREATE TABLE` only maps a table that exists to a name, and `DROP TABLE` is refused
(recorded: "drop table"). The connector has three methods for it, which use the signature and the rules of a statement
(D225 and W49):

```go
func (c *Connector) CreateTable(ctx context.Context, t Table, opts ...TableOption) error
func (c *Connector) DeleteTable(ctx context.Context, name string, opts ...DeleteOption) error
func (c *Connector) CreateIndex(ctx context.Context, table string, ix Index, opts ...IndexOption) error
```

A `Table` holds a name, `Keys` and `Columns` of `Column{Name, Type}`, and `Indexes` of `Index{Name, Keys, Columns,
Sync}`. The type is `Integer`, `Double`, `Boolean`, `String` or `Binary`, and a primary key column cannot be a `Double`
or a `Boolean`. An index is global. `Sync` asks the server to update it with each write, and with no `Sync` the server
fills it in the background. The methods do not retry. The calls fail with an `*Error` that matches `ErrExists` for HTTP
409 and the code `OTSObjectAlreadyExist`, and `ErrNotFound` for HTTP 404 and the code `OTSObjectNotExist`. This holds
for a table and for an index. A value that an option cannot hold fails the call with `dbimp.ErrInvalidValue`, and the
call sends nothing.

The options are variadic, and a later one wins. With none, a row never expires, the table keeps one version of a cell,
the reserved throughput is zero, and `CreateIndex` copies the rows that the table holds.

| Call | Option | Member of the request | Default |
| --- | --- | --- | --- |
| `CreateTable` | `WithTimeToLive(d)` | `TableOptions.time_to_live`, whole seconds | -1, which never expires |
| `CreateTable` | `WithMaxVersions(n)` | `TableOptions.max_versions` | 1 |
| `CreateTable` | `WithReservedThroughput(read, write)` | `reserved_throughput.capacity_unit` | 0 and 0 |
| `CreateTable` | `WithDeviation(d)` | `TableOptions.deviation_cell_version_in_sec`, whole seconds | not sent |
| `CreateTable` | `WithAllowUpdate(b)` | `TableOptions.allow_update` | not sent |
| `CreateTable` | `WithStream(d)` | `stream_spec`, whole hours | no stream |
| `CreateTable` | `WithLocalTransaction(b)` | `enable_local_txn` | off |
| `CreateIndex` | `WithBaseData(b)` | `include_base_data` | true |
| `DeleteTable` | `WithIfExists()` | none, the client ignores `ErrNotFound` | fails |

The methods offer no bloom filter, block size, encryption, partitions, `DropIndex` or local index. Ken decided that
none of them is needed.

Measured on 2026-10-30 on a new instance of `dbmeta` in ap-southeast-5, with the recordings
`testdata/tablestore/tablestore-tables-*.json` (release `tablestore-tables`):

- A table with a string and an integer key, one defined column of each of the five types and one index was made in
  0.25 to 0.55 seconds. `DescribeTable` showed the columns and the index at once.
- A `CREATE TABLE` mapping, an `INSERT` and a `SELECT` over that table worked at once, and a row came back as the
  Go types of D223.
- `DeleteTable` of a table that is not there is an error. A name was free again for `CreateTable` at once after
  `DeleteTable`.
- `CreateIndex` on a table that exists with `include_base_data` answered at once. The message for an index that is there
  names the index with an internal prefix, `<prefix>#<index>`.
- The server refuses a time to live under one day: `OTSParameterInvalid: TimeToLive cannot be less than 86400.` A table
  made with a time to live of one day, three versions and `WithAllowUpdate(false)` showed those values in
  `DescribeTable`. The recordings do not hold this call, and `TestIntegrationCreateTableOptions` makes it.
- The throughput, the deviation, the stream, the local transaction and the update mode of an index are checked on the
  wire against `table_store.proto`, by tests that read the bytes with their own parser. No live request used them.
- The content of an index is not measured, which Ken decided.

## Open questions

These wait for Ken. Do not decide them here.

1. Does the driver read a result larger than 2600 rows with `next_search_token`? Step 6 and the integration tests did
   not build a table large enough to show a paging limit, and the server never sent a token.
2. The driver reads a `DATETIME` and writes a `time.Time` in the zone Asia/Shanghai, which is a fact of the instance
   that the driver measured. Does the zone stay the same for every instance and region, or does the DSN need a key
   for it? D222 allows the key `tls` only.
3. The driver holds the rows that it has read, because FlatBuffers refers to any place of the message. A result of
   many megabytes then costs that much memory, which D25 forbids for JSON and D224 item 5 accepts for this driver. Does
   Ken want a limit on the size of an answer?

## Decided by Ken on 2026-10-29

1. The package and the registered name are `tablestore`.
2. The Go type of `DATETIME` is `dbimp.LocalDateTime`.

## Decided by Ken on 2026-10-30

1. The package makes and deletes a table with the methods `CreateTable`, `DeleteTable` and `CreateIndex` of
   `*Connector`, and not with a package `tablestore/admin` (D225).
2. Each method takes variadic options in the pattern of D109, so that a caller can change the time to live, the
   versions, the reserved throughput and the other members that the request has.
3. There is no `DropIndex` and no local index. The content of an index is not measured. The methods do not retry.
4. The release is `v0.22.0`.
