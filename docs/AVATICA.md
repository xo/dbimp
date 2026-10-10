# Avatica

This file holds what is known about Apache Calcite Avatica, for the driver
`avatica`, which D74 places ninth after Neo4j. Its work item is W24. The
headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a and 6 measured the standalone server 1.28.0 and 1.29.0 in front of
HSQLDB, as `SA` and as `dbmeta_user`, and the Phoenix Query Server 2.0-5.0,
as its one user, on 2026-10-01, each over JSON (D153). A fact marked
"recorded" is in `testdata/avatica/`, and the name in quotes after it is the
name of its request in `requests.json` there. A fact marked "measured" names
how it was measured. 1.28.0 and 1.29.0 gave the same answers. Each section
starts with the measured facts. The facts after them come from the sources
of step 3, and each one that is not measured says so. The sources, each read
on 2026-09-27 unless a line says otherwise, are these:

- "The JSON reference" is `site/_docs/json_reference.md` in
  `github.com/apache/calcite-avatica`.
- "The Go driver" is `github.com/apache/calcite-avatica-go/v5` v5.4.0,
  which `usql` uses: its code, its `docs/go_client_reference.md` and its
  `docker-compose.yml`. It was read again at commit `6e6fead` of 2026-06-01.
- "phoenixdb" is `github.com/lalinsky/python-phoenixdb`, the Python driver of
  the Phoenix Query Server, read on 2026-10-01.
- "GitHub" and "Docker Hub" are the tags that each one listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-09-27 and again on 2026-10-01.
- "The dbmeta entries" are `container/avatica.go` and `container/phoenix.go`
  in `dbmeta`, as dbmeta D155 changed them on 2026-10-01: each starts with
  JSON, and the standalone server has the ordinary user `dbmeta_user`, who
  can read `DBMETA.READABLE` and nothing else.

## Summary

- Avatica is a wire protocol, and not a database. A client sends JDBC calls
  to an Avatica server over HTTP, and the server runs each one on a
  database behind it through JDBC (the JSON reference). So one driver serves
  every product that speaks it, and each product is a flavor (D74).
- The driver serves two flavors: the standalone server of Avatica, and the
  Apache Phoenix Query Server. Druid gets a driver of its own (D154).
- The standalone server runs HSQLDB 2.4.1 (recorded: "the version").
- R holds. `dbrun` starts `avatica-1.28.0`, `avatica-1.29.0` and
  `phoenix-2.0-5.0`, each in the Staged tier with the cadence `tested`
  (measured with `dbrun list --json all` on 2026-10-01).
- H and S hold. Over JSON, `openConnection`, `createStatement`,
  `prepareAndExecute` and `closeConnection` answered on each release
  (recorded: "a statement").
- `dburl` has the scheme `avatica`, with the alias `phoenix` and the default
  URL `http://localhost:8765/`. Its `GoPackage` is the Go driver. `usql` has
  `drivers/avatica`, which imports the Go driver at v5.4.0. So a driver here
  can replace it (D24).
- `dbmeta` has no model for Avatica. Its D66 says that Avatica has no catalog
  of its own, because it stands in front of another database.

## Requests

These facts were recorded on each release:

- Each call is one `POST /` of one JSON object, whose field `request` names
  the call, such as `openConnection`. The answer names itself in `response`
  (recorded: "open a connection").
- A connection is `openConnection` with a `connectionId` that the client
  chooses, and `info` with `user` and `password`, which the server passes to
  the database (recorded: "open a connection" and "a wrong password"). An id
  that is open already is an error, `Connection already exists` (recorded on
  Phoenix, after a run that left one open).
- `createStatement` gives a `statementId`. `prepareAndExecute` runs a text
  on it, and `prepare` with `execute` runs a text with parameters. `fetch`
  reads the next frame, with `offset` and `fetchMaxRowCount`. `commit`,
  `rollback`, `connectionSync`, `databaseProperties`, `closeStatement` and
  `closeConnection` do what their names say (recorded).
- A field that a call does not know is an error, such as `frameMaxSize` on
  `fetch` and `maxRowsInFirstFrame` on `execute` (measured with `curl` on
  2026-10-01, and recorded: "an unknown field").
- A number sent as a JSON string, such as `"statementId": "3"`, is read as
  the number (measured with `curl` on 2026-10-01).
- A `prepareAndExecute` on a statement that the connection does not have runs
  nothing, and answers `missingStatement: true` with no result and no error
  (measured with `curl` on 2026-10-01).
- `databaseProperties` gives the metadata of JDBC, such as the functions of
  the database (recorded: "the properties of the database").
- The Phoenix Query Server reads the raw bytes of a request wrongly outside
  ASCII: `'é'` in the text arrived as U+FFFD, and `'\u00e9'`, the escape of
  JSON, arrived as `é`. The standalone server reads both (measured with
  `curl` on 2026-10-01).
- A request with `Accept-Encoding: gzip` gets no gzip (recorded: "a gzip
  answer").

These facts come from the sources, and are not measured:

- Authentication is none, HTTP Basic, HTTP Digest or SPNEGO with Kerberos,
  by the choice of the server (the Go driver and Gemini).
- The protobuf form holds each message in a `WireMessage` (the Go driver).
  The driver speaks JSON only (D153).

## The DSN

- Step 9 decides the URL (D27 and D35). The Go driver takes
  `http://host:port[/schema][?key=value]`, with the keys `authentication`,
  `avaticaUser`, `avaticaPassword`, `principal`, `keytab`, `krb5Conf`,
  `krb5CredentialsCache`, `location`, `maxRowsTotal`, `frameMaxSize`,
  `transactionIsolation` and `batching` (the Go driver). Its scheme is
  `http`, which D35 does not allow here.
- The dbmeta entries give each principal as `http://user:password@host:port`,
  with `SA` and no password for the standalone server, and `phoenix` for the
  Phoenix Query Server.

## Responses

These facts were recorded on each release:

- A statement answers `executeResults` with `results`, one `resultSet` for
  each result, which holds `signature`, `firstFrame` and `updateCount`
  (recorded: "a statement"). A statement with no rows, such as DDL, has a
  `signature` of null and its count in `updateCount` (recorded: "crud:
  insert").
- `signature.columns` names each column, with `label`, `columnName`,
  `nullable`, `precision`, `scale`, and `type` with the id of JDBC, its
  `name` and its `rep`, before the rows (recorded: "every type"). So rule 1
  of D18 applies. Two columns can have one label (recorded: "two columns
  with one name").
- A frame is `offset`, `done` and `rows`, an array of arrays in the order of
  the columns (recorded).
- `prepareAndExecute` with no `maxRowsInFirstFrame` answers a first frame
  with no rows and `done: true`, on each flavor. So a client that trusts
  `done` reads no rows (recorded: "a statement with no first frame"). With
  `maxRowsInFirstFrame`, the first frame holds that many rows, and `done` is
  false when more follow (recorded: "a result of 250 rows, 100 in the first
  frame"). `execute` takes no `maxRowsInFirstFrame`, and its first frame
  holds the rows (recorded: "execute with typed parameters").
- `fetch` from the `offset` after the last row read gives the next frame,
  and `done: true` on the last one. A `fetch` after the last frame is an
  error, `invalid cursor state: identified cursor is not open` (recorded:
  "fetch the second frame", "fetch the last frame" and "fetch after the last
  frame").
- `maxRowCount` cuts a result at that many rows, with `done: true` (recorded:
  "a result with a limit on the rows").
- A text with two statements on the standalone server gives the result of
  the last one only. Phoenix refuses it (recorded: "two statements in one
  text").

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table,
Ken reviewed it on 2026-10-01 (D155), and `avatica/tables_test.go` writes it
from the code (step 10).

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INTEGER | integer | `int64` | `int64` | `INTEGER` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| UNSIGNED_INT | integer | `int64` | `int64` | `UNSIGNED_INT` | yes |
| UNSIGNED_LONG | integer | `int64, because Phoenix keeps it from 0 to 9223372036854775807` | `int64` | `UNSIGNED_LONG` | yes |
| DECIMAL | decimal | `*apd.Decimal, from the digits of the JSON number` | `*apd.Decimal` | `DECIMAL` | yes |
| DOUBLE | float | `float64, with the infinities and NaN from their strings` | `float64` | `DOUBLE` | yes |
| FLOAT | float | `float64` | `float64` | `FLOAT` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| CHAR | string | `string, padded as the server sends it` | `string` | `CHAR` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| BINARY | binary | `[]byte, from base64` | `[]uint8` | `BINARY` | yes |
| VARBINARY | binary | `[]byte, from base64` | `[]uint8` | `VARBINARY` | yes |
| DATE | date | `dbimp.Date, from the days since 1970-01-01` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime, from the milliseconds since midnight` | `dbimp.LocalTime` | `TIME` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime, from the milliseconds since 1970-01-01 in UTC` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| INTERVAL | interval | `dbimp.Interval, from the text of HSQLDB` | `dbimp.Interval` | `INTERVAL` | yes |
| ARRAY | array | `[]any, of the Go types of its elements` | `[]interface {}` | `ARRAY` | yes |
| UUID | uuid | `uuid.UUID, from its text` | `uuid.UUID` | `UUID` | yes |
<!-- /dbimp:types -->

The driver reads each value by the name of its type, and not by its `rep`,
because the two disagree: a `UUID` has the `rep` `BYTE_STRING` and arrives as
its text, and a `DECIMAL` of Phoenix has the `rep` `OBJECT` (recorded:
"every type"). `TIMESTAMP WITH TIME ZONE`, `TIME WITH TIME ZONE`, `CLOB` and
`BLOB` have no row, because the server cannot write them in JSON (below).

These facts were recorded on each release, from a table with one column of
each type (recorded: "every type"):

- An integer is a JSON number, and keeps every digit to
  -9223372036854775808 and 9223372036854775807. A `DECIMAL` is a JSON number
  with every digit, such as `1234567890123456789012345678.0123456789`.
- A `DOUBLE` is a JSON number, and an infinity is the string `"Infinity"`
  (recorded: "an infinity"). On HSQLDB a `REAL` is a `DOUBLE`. On Phoenix, a
  `FLOAT` holds a float32, so `3.4E38` reads as `3.3999999521443642E+38`,
  and an infinity fails with `NumberFormatException`.
- A `BOOLEAN` is a JSON boolean. A `CHAR(3)` of `'ab'` is `"ab "` on HSQLDB
  and `"ab"` on Phoenix. Phoenix stores an empty string as NULL.
- `BINARY` and `VARBINARY` are strings in base64.
- A `DATE` is the number of days since 1970-01-01, such as 20727 for
  2026-10-01, and -719164 for 0001-01-01. A `TIME` is the milliseconds since
  midnight: HSQLDB gave 45296000 for `TIME '12:34:56.123456'`, so it keeps
  whole seconds, and Phoenix gave 45296789 for 12:34:56.789. A `TIMESTAMP` is
  the milliseconds since 1970-01-01 of the time as if it were in UTC:
  `TIMESTAMP '2026-10-01 12:34:56.123456789'` gave 1790858096123, so it keeps
  milliseconds.
- An `INTERVAL DAY TO SECOND` of HSQLDB is its text, such as
  `"1 02:03:04.500000"`, with the name `INTERVAL DAY TO SECOND` and the `rep`
  `STRING`.
- An `INTEGER ARRAY` is a JSON array, such as `[1,null,3]`, with the `rep`
  `ARRAY`.
- A `UUID` of HSQLDB is its text.
- On Phoenix, a NULL in a `BINARY(2)` column reads as two zero bytes, and
  not as NULL (measured by `TestIntegrationRoundTrip` on 2026-10-01). The
  driver returns what the server sends.
- The literal `INTERVAL '-0 00:00:00.000001' DAY TO SECOND` read back as
  `"0 00:00:00.000001"`, so the sign of a day of 0 is lost before the driver
  reads it (recorded: "every type"). `'-2 00:00:01'` keeps its sign
  (measured by `TestIntegrationRoundTrip`).
- A `DATE` before 1582-10-15 is a day of the Julian calendar of Java.
  `DATE '0001-01-01'` gave -719164, which the driver reads as 0000-12-30 of
  the Gregorian calendar (recorded: "every type"). See the open questions.
- A `CLOB` or a `BLOB` fails the whole answer with HTTP 500, because the
  server has no serializer for them (recorded: "a CLOB and a BLOB"). So does
  a `TIMESTAMP WITH TIME ZONE` and a `TIME WITH TIME ZONE`, whose Java types
  its JSON encoder does not know (recorded: "a timestamp with a zone" and "a
  time with a zone").

## Parameters

These facts were recorded on each release:

- `prepare` gives a statement handle with a `signature` that names each
  parameter by its type. `execute` takes the whole handle, and
  `parameterValues`, a list of `TypedValue` with the `type` of a `rep` and a
  `value`. A handle without its `signature` fails with
  `NullPointerException` (measured with `curl` on 2026-10-01, and recorded:
  "execute with typed parameters").
- The `rep` of a value is `INTEGER`, `LONG`, `DOUBLE`, `STRING`, `BOOLEAN`,
  `NUMBER`, `BYTE_STRING` in base64, `JAVA_SQL_DATE` in days,
  `JAVA_SQL_TIME` in milliseconds, `JAVA_SQL_TIMESTAMP` in milliseconds, or
  `NULL` (measured with `curl` on 2026-10-01). `BIG_DECIMAL` is no `rep`.
- A `NUMBER` must be a JSON number, and a string fails. The server reads it
  as a double, so `1234567890123456789012345678.0123456789` arrived as
  `1234567890123456900000000000` (recorded: "execute with typed parameters"
  and "execute with a decimal in a string").
- Too few values bind the rest as NULL, with no error, on HSQLDB and on
  Phoenix (recorded: "execute with too few parameters").
- The `STRING` `"12"` for an `INTEGER` binds as 12 on HSQLDB, and fails on
  Phoenix with `Type mismatch` (recorded: "execute with a string for an
  integer").
- Phoenix fails `prepare` of a `CAST(? AS ...)` with `NullPointerException`
  in its metadata of parameters (recorded: "a prepare that Phoenix cannot
  describe"). A parameter whose type a column gives prepares.
- A statement names each parameter as `?`. There are no named parameters
  (the JSON reference).

## Transactions

These facts were recorded as the administrator:

- The standalone server opens each connection with `autoCommit` true, and the
  Phoenix Query Server with `autoCommit` false. The isolation is 2 on HSQLDB
  and 4 on Phoenix (recorded: "the properties of the connection").
- `connectionSync` with `autoCommit` false starts a transaction. A read on
  the connection sees its insert on HSQLDB, and does not on Phoenix, and
  `rollback` removes it on both. `commit` keeps the next insert (recorded:
  "read in the transaction", "read after the rollback" and "read after the
  commit").
- `connectionSync` with `readOnly` true makes HSQLDB refuse an insert with
  `invalid transaction state: read-only SQL-transaction` (recorded: "a
  read-only connection").

## Errors

These facts were recorded on each release:

- An error answers HTTP 500 with `{"response": "error", "exceptions": [...],
  "errorMessage": ..., "errorCode": ..., "sqlState": ..., "severity": ...}`.
  `exceptions` holds the Java stack traces. `errorMessage` joins the causes,
  such as `RuntimeException: java.sql.SQLSyntaxErrorException: unexpected
  token: SELEC -> ...`. `sqlState` is `00000` for an error of HSQLDB
  (recorded: "a syntax error").
- An unknown connection gives `NoSuchConnectionException` with
  `errorCode` 1. A `fetch` of an unknown statement answers HTTP 200 with
  `missingStatement` and `missingResults` true, and no frame (recorded: "an
  unknown connection" and "an unknown statement").
- A wrong password fails `openConnection` with `invalid authorization
  specification`, and the connection does not exist after it (recorded: "a
  wrong password" and "a statement with the wrong password").
- An error while a statement makes its first frame, such as a division by
  zero in its fourth row, gives the error and no rows (recorded: "an error
  inside the rows").
- HSQLDB makes the whole result before it sends the first frame. A division
  by zero in the sixth row, with frames of three rows, gave the error and no
  rows (measured by `TestIntegrationErrorInResult` on 2026-10-01). So on
  HSQLDB, an error never follows some rows.

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose first
exception names `SQLInvalidAuthorizationSpecException`. That is the answer to
"a wrong password" and to "a statement with the wrong password". An HTTP 401
also matches, through the `*dbimp.StatusError` that the `*Error` wraps.

It is false for every other exception, such as the `SQLSyntaxErrorException`
of "a syntax error", which HSQLDB also sends for "user lacks privilege or
object not found", and for `NoSuchConnectionException`.

## Cancellation and timeouts

- A query does not stop when the client leaves. On 1.29.0, a query that the
  client left after one second used six seconds of processor time in the
  next nine (measured from `/proc/<pid>/stat` of the server on 2026-10-01).
  The next statement on the connection answered at once (recorded: "a query
  after the client left").
- `closeStatement` of a running query answered at once, and the query ran on
  to its end, and then failed with `cursor is not open` (measured with
  `curl` on 2026-10-01). So neither flavor has a way to stop a statement.
- The server keeps each connection and statement in a cache that expires
  after 10 minutes by default (Gemini, not measured).

## Statements

- Comments `--` and `/* */` are part of the text (recorded: "comments").
- A statement that ends with a semicolon runs on HSQLDB (recorded).

## Principals

- On the standalone server, `dbmeta_user` reads `DBMETA.READABLE`, and is
  refused every other table and every write, with `user lacks privilege or
  object not found` (recorded as `dbmeta_user`: "a table that the user was
  not granted" and "crud: make the tables").
- The Phoenix Query Server checks no user without Kerberos, so its entry has
  no ordinary user (dbmeta D155).
- `VALUES (DATABASE_VERSION())` gives the version of HSQLDB to both users
  (recorded: "the version").
- The driver does not answer `SELECT version()` itself (D181), and the
  decision for a driver with two flavors is an open question. On
  `avatica-1.29.0` on 2026-10-08, `VALUES (DATABASE_VERSION())` gave the
  version of HSQLDB, and the request `databaseProperties` gave `2.4.1` as
  `GET_DATABASE_PRODUCT_VERSION` to `SA` and to `dbmeta_user`. On
  `phoenix-2.0-5.0` on 2026-10-08, `SELECT version()`, `SELECT
  DATABASE_VERSION()` and `VALUES (DATABASE_VERSION())` all failed, so
  Phoenix has no SQL for the release, and `databaseProperties` gave `5.0` to
  the user `phoenix`, the only user of that entry. The driver never sends
  `databaseProperties`, and `openConnection` carries no release.

## Flavors

- The standalone server runs HSQLDB, whose SQL has `VALUES`, `MERGE` and
  sequences (recorded: "crud: merge" and "schema: sequence read").
- Phoenix writes with `UPSERT`, and refuses `INSERT`, `UPDATE`, a foreign key
  and a check constraint with a syntax error (recorded: "crud: insert",
  "crud: update", "schema: foreign key" and "schema: check constraint"). It
  has salted tables and row timestamps (recorded).
- `databaseProperties` names the database of each flavor. The driver does
  not tell the flavors apart, and sends the SQL of the caller (D156).
- On Phoenix, an `UPSERT` into a table with a `ROW_TIMESTAMP` column fails
  with `ArrayIndexOutOfBoundsException` when it leaves out that column. An
  `UPSERT` that names it works (measured by `TestIntegrationFeatures` on
  2026-10-01).

## Interfaces

`avatica/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping sends connectionSync with no change, which checks that the server knows the connection. |
| `driver.SessionResetter` | no | database/sql ends a transaction before it reuses a connection, and the end sets autoCommit and the other properties back (D159). |
| `driver.Validator` | yes | A connection that the server no longer knows is not valid, and database/sql closes it. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64, an *apd.Decimal, a uuid.UUID and the civil types of the root package, which the driver writes itself (D158). |
| `driver.QueryerContext` | yes | A query sends prepareAndExecute, or prepare and execute with arguments, and reads its rows in frames (D157 and D158). |
| `driver.ExecerContext` | yes | Exec runs as a query, and gives updateCount as the count of rows. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx sends connectionSync with autoCommit false, and with readOnly and transactionIsolation from the options (D159). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | The driver reads the first result only. HSQLDB gives the result of the last statement of a text, and Phoenix refuses a text of two. |
| `driver.RowsColumnTypeScanType` | yes | The signature names the type of each column, which gives the Go type (D155). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The signature names the type of each column, such as CHARACTER or INTEGER ARRAY. |
| `driver.RowsColumnTypeLength` | yes | The signature gives the precision of a text or a binary column, which is its length. |
| `driver.RowsColumnTypeNullable` | yes | The signature says whether each column can hold NULL, or that the server does not know. |
| `driver.RowsColumnTypePrecisionScale` | yes | The signature gives the precision and the scale of a DECIMAL. |
<!-- /dbimp:interfaces -->

## Faults

These are faults of the Go driver that a driver here does not repeat (the
Go driver):

- A `fetch` of the next frame uses `context.Background`, so the context of
  the caller does not stop it (`rows.go`).
- Each response is read whole with `io.ReadAll` (`http_client.go`).
- Its DSN is an `http` URL, so its scheme is not the name of the driver
  (D35).
- It needs protobuf, a Kerberos library and a digest library (`go.mod`),
  where D13 allows the standard library, apd and a binary encoding that Ken
  approves.
- It speaks protobuf only, so a person cannot read a request (Gemini).
- It keeps no cookies, so a load balancer with sticky sessions can send a
  call to another server, which does not know the connection (Gemini).
- The zone of a time is taken from the key `location`, not from the server
  (the Go driver).

## Second opinions

Step 7 tested each lead on the server. Gemini and DeepSeek were asked the
same questions on 2026-10-01.

- CRUD. Both said that Phoenix has `UPSERT` and no `INSERT`, `UPDATE` or
  `MERGE`. The server agrees.
- Schema. Gemini said that Phoenix has no foreign key and no check
  constraint, and DeepSeek was unsure. The server refuses both.
- Dates. Gemini said that a `DATE` is days and a `TIMESTAMP` milliseconds,
  and DeepSeek that each is ISO text. The server sends numbers. Gemini was
  right.
- A `DECIMAL`. Both said that it can come as a string. It comes as a JSON
  number, with every digit. As a parameter, it loses digits.
- A `UUID`. Neither named it. It comes as its text, with the `rep`
  `BYTE_STRING`.
- A client that leaves, and `closeStatement`. The server runs a query to its
  end either way.

Step 8a asked both models on 2026-10-01 to review the mapping of the types:

- Both agreed with the integers, the decimal, the floats with their strings,
  the boolean, the strings, the bytes, the date, the time of day and the
  array. Both agreed that a `TIMESTAMP` is a local timestamp, read from its
  milliseconds in UTC and with no zone.
- Gemini said that an `INTERVAL` of days and seconds and one of years and
  months differ, and that the driver refuses a qualifier it does not know.
  DeepSeek agreed with the interval.
- Gemini said that a `UUID` is a `string` or a `[16]byte`, to keep a package
  out of the driver. `uuid.UUID` is in the standard library (D25).
  DeepSeek agreed with it.
- DeepSeek said that the four types that the server cannot write keep a
  mapping, such as `time.Time` for `TIMESTAMP WITH TIME ZONE`, because the
  failure is the server's. Gemini agreed that they have none.

## Open questions

Ken decided D155 to D159 on 2026-10-01. Step 10 wrote the code and step 14
ran the tests, and these questions came up. Ken answered each of them the
same day:

1. A `DATE` before 1582-10-15 arrives in the Julian calendar of Java. Ken
   decided that the driver does not convert it (D160).
2. The driver binds no `ARRAY` and no `INTERVAL`. Ken decided that it does
   not (D160).
3. The workflow tests the releases whose product is the name of a folder of
   a driver. The product of `phoenix-2.0-5.0` in `dbmeta` is `phoenix`, so
   CI did not test the driver on Phoenix. Ken decided that the workflow
   tests each product that the manifest of a driver recorded (D161).
4. A connection, a transaction and the rows of a query keep a context, for
   the requests that a method with no context sends. Ken accepted these
   exceptions to hard rule 4.
5. The end of a transaction sets `readOnly` and `transactionIsolation`
   back to their values before it, where D159 names `autoCommit` only. If
   the driver did not, the next statement outside the transaction would be
   read-only. Ken added this to D159.

Open, from D181 on 2026-10-08:

6. The standalone server runs HSQLDB, which has a query for the release, and
   the Phoenix Query Server has none, but gives the release to the request
   `databaseProperties` (Principals). D181 gives no support to a product that
   has a query, and it does not say what to do for a driver with two
   flavors. The driver adds nothing. Ken decided on 2026-10-08 that Avatica
   gets no support on either flavor (D181 item 10).

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-10-01 from the staged code. A fact of Couchbase comes
from [COUCHBASE.md](COUCHBASE.md), and a fact of Avatica from the sections
above.

### The server

| | Couchbase | Avatica |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /` with one call of JSON, such as `prepareAndExecute` or `fetch`, on a connection and a statement that earlier calls opened (Requests) |
| Database | The key `query_context` of the body | The database behind the server. A statement names the schema of each table |
| Language | SQL++, which is close to SQL | The SQL of the database behind the server: HSQLDB, or Phoenix with `UPSERT` (Flavors) |
| DDL | In SQL++ | In SQL |
| Parameters | `?`, `$1` and `$name` | `?` only, bound by `execute` with a `TypedValue` for each (Parameters) |
| Framing | One body for the whole result, which does not page | Frames: the first one in the answer to the statement, and each next one from `fetch` (Responses) |
| Columns | `signature`, before the first row | `signature` of the result, before the first frame |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | HTTP 500 with the error, before any row of the answer. `missingStatement` comes with HTTP 200 and no error (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, read by the name of the type of JDBC: a date in days, a time in milliseconds, a decimal with every digit, bytes in base64. A zone, a CLOB and a BLOB fail the whole answer (Types) |
| Cancel | The server stops a query when the client leaves | Nothing stops a statement. The server runs it to its end (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `connectionSync` with `autoCommit` false, then `commit` or `rollback` on the connection (Transactions) |
| Authentication | Basic, or `creds` in the body | `user` and `password` in the info of `openConnection`, and HTTP basic for a server that asks for it |
| Default port | 8093, or 18093 with TLS | 8765 |

The differences that a caller sees:

- A connection of `database/sql` is a connection on the server, which
  `closeConnection` ends, where Couchbase holds nothing between requests
  (D157).
- A statement with arguments costs two requests, `prepare` and `execute`,
  and the driver refuses a count of arguments that is not the count of the
  parameters (D158).
- A query that the caller leaves runs on to its end on the server (D159).
- A decimal argument goes as text, and Phoenix refuses it (D158).
- `INSERT` and `UPDATE` fail on Phoenix, which writes with `UPSERT`
  (Flavors).

### The driver

| | `couchbase` | `avatica` |
| --- | --- | --- |
| Size, without tests, on 2026-10-01 | About 1300 lines in 8 files | About 1900 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Auth` (D156) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithFrameSize`, through `WithOptions` or an argument (D109). A timeout above zero, `WithReadonly(true)` and `WithDatabase` give `dbimp.ErrNotSupported`. `WithParameter` sets any key of the call that runs the statement (`options.go`) |
| Arguments | Sent to the server as `args` and `$name` | Sent as a `TypedValue` each, after `prepare`. A named argument is an error (D158) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A reader of the frames, one token at a time, which fetches the next frame and closes the statement after the last row (`rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, `ColumnTypeScanType`, `ColumnTypeNullable`, `ColumnTypeLength` and `ColumnTypePrecisionScale`, from the signature (D155) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the name of the type: `int64`, `*apd.Decimal`, `float64`, `bool`, `string`, `[]byte`, `dbimp.Date`, `dbimp.LocalTime`, `dbimp.LocalDateTime`, `dbimp.Interval`, `[]any` and `uuid.UUID` (D155) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `updateCount`. `LastInsertId` gives `dbimp.ErrNotSupported` (`conn.go`) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` sends `connectionSync` with `autoCommit` false, `readOnly` and `transactionIsolation`. The end sends `commit` or `rollback`, then sets the properties back (D159) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | `IsValid`, which is false once the server does not know the connection. No `ResetSession` |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The request carries the context, which stops the read. The driver then sends `closeStatement` with a limit of its own (D159) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Message, Code, SQLState, Exception}`, which unwraps to `*dbimp.StatusError`. An unknown connection is `driver.ErrBadConn` (`errors.go`) |
| Authentication | Basic | The info of `openConnection`, and basic too with `auth=basic` (D156) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `AuthNone` and `AuthBasic` |

The differences that a caller sees:

- A value has the Go type of the name of its type of JDBC, where Couchbase
  gives JSON shapes (D155).
- A read-only transaction and an isolation level reach the server, where
  Couchbase sends `readonly` only (D159).
- `WithTimeout` above zero fails, because the server has no timeout and runs
  each statement to its end (D159). The context still ends the request. No
  decision names the failure of the option itself.
- `LastInsertId` fails, because Avatica gives no id. No decision names it.
- An unknown connection, such as one whose entry in the cache of the server
  expired, is `driver.ErrBadConn`, so `database/sql` opens another one (hard
  rule 5).
