# Snowflake

This file holds what is known about Snowflake over its SQL REST API, for a
possible driver `snowflake` (W34 and D182). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with the date, or "not measured", with its source.

Step 6 recorded the service on 2026-10-09 as one login, `<user>`, with the role
`<role>`, which `dbsetup` made for this job (dbmeta D117). The recorder sent
every request with a key-pair JWT as a Bearer token. The recordings are under
`testdata/snowflake/`. The account name, the account locator, the user and the
role are replaced by `<account>`, `<locator>`, `<user>` and `<role>` in every
recorded file and in `requests.json`. The script holds `<role>` where the role
goes, and a run must put the real role there first. The service has no
release that a person picks, and the version was `10.36.101` in the region
`AWS_AP_SOUTHEAST_3` (recorded: "the session facts").

## Summary

- Product: Snowflake, a hosted data warehouse. Name in `dbrun`: `snowflake`.
  The kind is `hosted` and the tier is `verified` (measured, `dbrun list all`,
  2026-10-08). `dbrun` lists the entry only while a person has supplied a
  connection string (dbmeta D117).
- R: a hosted service has no container, so `dbrun` cannot start it. The
  account is a trial that ends after 30 days or when its credit of 400 dollars
  is used (D182). CI would need a secret that holds a private key and would
  spend credit on every run. Whether CI can hold that secret is not measured.
  This is the one condition of R that the product does not meet.
- H: `POST /api/v2/statements` is HTTPS and JSON, and it answered every
  statement that the login can run (recorded: "a statement"). It needs no
  binary encoding. A result is JSON text, and a large result is a list of JSON
  partitions that the client fetches with `GET`.
- S: SQL. It answered `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `MERGE` and DDL
  (recorded: "a merge and its counts", "a create table and its answer").
- Whether it can be a driver: yes, over HTTP and JSON, with no Arrow. No
  condition of "When it cannot be a driver" holds. The result is complete: the
  server runs the whole statement before it sends the first partition, so an
  error comes before any row (recorded: "an error after some rows were
  produced"). There is no cut result. The facts that make it costly are the
  hosted account, the signing of the JWT, and the rule that `BEGIN` alone is
  refused.
- Scheme in `dburl`: `Name` is `snowflake`, the alias is `sf`, the generator is
  `GenSnowflake`, and the `Dialect` is `snowflake`. `GoPackage` is
  `github.com/snowflakedb/gosnowflake/v2`. `GenSnowflake` writes
  `user:pass@host:port/dbname?options`, which is the DSN of `gosnowflake`
  (read of `dburl/scheme.go` and `dburl/dsn.go`, 2026-10-08).
- Driver of `usql` now: `snowflakedb/gosnowflake/v2` v2.2.0. It speaks the
  private protocol of Snowflake, with `/session/v1/login-request` and
  `/queries/v1/query-request`, and it downloads large results as Arrow chunks
  (read of the source of v1.18.1, 2026-10-08). Its `Version` statement is
  `SELECT CURRENT_VERSION()`, and that statement works on the SQL API too
  (recorded: "the version").
- What `dbmeta` has: `models/snowflake`, written from the documentation and
  never run before 2026-10-08 (dbmeta D144).

## Requests

- A statement is `POST /api/v2/statements` with a JSON body. The members that
  this work used are `statement`, `timeout`, `warehouse`, `role`, `database`,
  `schema`, `parameters` and `bindings` (recorded: "a statement", "a binding of
  the type FIXED").
- The headers are `Authorization: Bearer <token>`,
  `X-Snowflake-Authorization-Token-Type: KEYPAIR_JWT`,
  `Content-Type: application/json` and `Accept: application/json`. A request
  with no `Content-Type` of JSON fails with HTTP 400 (recorded: "a request with
  another content type"). A request with no token type header still works,
  because the server reads the type from the token (measured, 2026-10-09).
- The JWT is signed with RS256. The claims are `iss`
  `<ACCOUNT>.<USER>.SHA256:<fingerprint of the public key>`, `sub`
  `<ACCOUNT>.<USER>`, `iat` and `exp`, with the account written as
  `<org>-<account>`. The driver cuts the host at the first dot (question 13).
  The lifetime is at most one hour, and `dbsetup` used 3300 seconds. The form with the account locator did not work for the login
  through `gosnowflake`, as `dbsetup` reported (measured by `dbsetup`,
  2026-10-09).
- The host is `<org>-<account>.snowflakecomputing.com`.
- A statement waits on the server up to its `timeout` in seconds. The query
  `?async=true` returns at once with HTTP 202 (see Cancellation and timeouts).
- `?requestId=<uuid>&retry=true` makes the server treat a second request with
  the same id as a retry of the first. Both answered the statement with HTTP
  200 (recorded: "a request that is retried with the same request id" and
  "the same request id again"). Whether the second one runs the statement
  again is not measured.
- `PUT` and `GET` of stage files are refused: `Command not supported by SQL
  API: PUT`, code `391911` (recorded: "put a file into a stage").

## The DSN

Not decided. These are the facts that a DSN must carry:

- The account host, such as `<org>-<account>.snowflakecomputing.com`, which
  also names the account in the claims of the JWT.
- The login user, and a private key to sign the JWT. `dbrun` prints the key as
  the query key `privateKey`, a base64url text of the PKCS8 DER bytes, about
  1,600 characters for an RSA key of 2048 bits (measured, the size of the file
  that `dbsetup` read, 2026-10-09). D94 says that the secret of a driver here is
  the password of the URL, so a key in a query key breaks that rule, and the
  choice is for step 9.
- The `role`, the `warehouse`, the `database` and the `schema`, which go in
  the body of each request, not in the URL of the request.
- The lifetime of a JWT is at most one hour, so the driver must sign a new one
  before it expires. A request with an expired token answers HTTP 401, code
  `390144`, `JWT token is invalid` (recorded: "a wrong token", with a token
  that is not valid).
- Other kinds of token exist (`OAUTH` and `PROGRAMMATIC_ACCESS_TOKEN` as the
  value of the token type header). They are not measured, source: Gemini.

## Responses

- A finished statement answers HTTP 200 with a JSON object. The members are
  `resultSetMetaData`, `data`, `code`, `sqlState`, `message`,
  `statementHandle`, `statementStatusUrl`, `requestId` and `createdOn`
  (recorded: "a statement").
- `resultSetMetaData` has `numRows`, `format` (`jsonv2`), `partitionInfo` and
  `rowType`. Each `rowType` entry has `name`, `type`, `precision`, `scale`,
  `length`, `byteLength`, `nullable`, `database`, `schema`, `table` and
  `collation` (recorded: "columns of a result with rows").
- The names of the columns and their order come from `rowType`, which the
  answer carries also when the result has no rows (recorded: "columns of a
  result with no rows"). Two columns can share a name (recorded: "columns that
  share a name"). A column name is upper case unless the statement quotes it
  (recorded: "a column in lower case").
- `data` is an array of rows, and each row is an array of values. Every value
  is a JSON string, or JSON `null` for a NULL (recorded: "every type"). The
  server runs the whole statement first, so the answer arrives complete: the
  first partition holds the first rows, and the others are fetched on demand.
- The server sends the first partition in the answer and the others in a list
  `partitionInfo` that names the number of rows and the sizes of each one. The
  sizes grow: 20,000 rows made four partitions of 2,063, 4,682, 9,404 and
  3,851 rows (recorded: "a result of 20000 rows"). A statement of 120,000
  rows made six partitions that went up to 51,422 rows (measured, 2026-10-09).
  The first partition is plain JSON in the answer.
- A later partition is `GET /api/v2/statements/<handle>?partition=N`. It
  answers HTTP 200 with `Content-Encoding: gzip` (measured with `curl`,
  2026-10-09), and the JSON has only `data` (recorded: "partition 1 of the large
  result", where the Go client had decoded the gzip). A partition outside the range
  answers HTTP 400, code `391922`, and the message names the range (recorded:
  "a partition that does not exist" and "an invalid partition").
- The status of a finished statement is `GET /api/v2/statements/<handle>`. It
  returns the first partition again (recorded: "the status of a finished
  statement").
- A string of one million characters came in one value, so the cap on a value
  is not met there (recorded: "a result with the longest string"). A `text`
  column of a table says `length` 16777216, and the result of `REPEAT` says
  134217728.
- The answer is compressed with gzip when the request accepts it (recorded:
  "a request that accepts gzip").

## Types

The type name is the `type` member of the `rowType` entry. Each value is text.

- `fixed` has `precision` and `scale`. A value is the decimal digits, such as
  `12345678.91`, up to 38 digits (recorded: "every type", "a decimal beyond
  38 digits", "an integer beyond int64"). A scale of 0 with a precision of 18
  or less fits an `int64`.
- `real` is the text of a double, such as `1.5`. A NaN is `NaN`, and an
  infinity is `inf` or `-inf` (recorded: "float special values").
- `text` is a UTF-8 string, with `length` up to 16,777,216 (recorded: "every
  type").
- `binary` is hex text in upper case, such as `DEADBEEF` (recorded: "every
  type").
- `boolean` is `true` or `false`. In the last `SELECT` of a piped statement
  (`SHOW ... ->> SELECT ... FROM $1`) the same column is `1` or `0`, with the
  type still `boolean` (reported by `dbmeta`, measured through this driver on
  2026-10-10). The driver reads both forms.
- `date` is the number of days since 1970-01-01, such as `20735` and `-25567`
  for 1900-01-01 (recorded: "every type" and "a date before 1970").
- `time` is seconds with a fraction, such as `45296.123456789`, and `scale`
  gives the digits of the fraction.
- `timestamp_ntz` and `timestamp_ltz` are the seconds since the epoch with a
  fraction, such as `1791549296.123456789`. The NTZ value is a wall clock as if
  it were UTC, and the LTZ value is an instant.
- `timestamp_tz` is `<epoch seconds> <offset>`, where the offset is the minutes
  from UTC plus 1440. The text `1791524096.123456789 1860` is 420 minutes, which
  is +07:00.
- `variant`, `object` and `array` are JSON text, written over several lines
  with two spaces. A NULL inside an array is written `undefined` (recorded:
  "every type"). A variant can hold any JSON value, such as a number, a
  string, `null`, an array or an object (recorded: "a variant of each kind").
- `geography` and `geometry` arrive with the type name `object`, and the value
  is GeoJSON text. A geometry writes its coordinates in exponent form, such as
  `3.000000000000000e+00` (recorded: "every type").
- `vector` is text such as `[1.000000,2.000000,3.000000]`. The `rowType` gives no
  type of the elements and no length (recorded: "a vector").
- A NULL is JSON `null` in every type (recorded: "every type", the row of
  NULLs).
- The parameters of the statement change the text of a value. With
  `DATE_OUTPUT_FORMAT` set to `DD/MM/YYYY`, a `date` column arrives as
  `09/10/2026`, with the type still `date`. A `TIMESTAMP_NTZ_OUTPUT_FORMAT`
  changes a timestamp the same way (recorded: "a date output format of the
  statement" and "a timestamp output format of the statement"). A driver must
  not send these parameters, or it must read the format it sent.
- The session time zone of the account is America/Los_Angeles. `CURRENT_TIMESTAMP`
  as a string is `2026-10-08 15:37:50.242 -0700`, and the parameter `TIMEZONE`
  of one statement changes it to `Z` (recorded: "a timestamp with the zone of
  the session" and "a parameter of the statement").
- An interval is not a type here. The difference of two timestamps is a
  `fixed` number of seconds (recorded: "an interval").

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| FIXED INTEGER | integer | `int64` | `int64` | `FIXED` | yes |
| FIXED DECIMAL | decimal | `*apd.Decimal` | `*apd.Decimal` | `FIXED` | yes |
| REAL | float | `float64` | `float64` | `REAL` | yes |
| TEXT | string | `string` | `string` | `TEXT` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIME | time of day | `dbimp.LocalTime` | `dbimp.LocalTime` | `TIME` | yes |
| TIMESTAMP_NTZ | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP_NTZ` | yes |
| TIMESTAMP_LTZ | timestamp | `time.Time` | `time.Time` | `TIMESTAMP_LTZ` | yes |
| TIMESTAMP_TZ | timestamp | `time.Time` | `time.Time` | `TIMESTAMP_TZ` | yes |
| BINARY | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| VARIANT | json | `the decoded JSON value` | `interface {}` | `VARIANT` | yes |
| OBJECT | map | `map[string]any` | `map[string]interface {}` | `OBJECT` | yes |
| ARRAY | array | `[]any` | `[]interface {}` | `ARRAY` | yes |
| GEOGRAPHY | geometry | `map[string]any` | `map[string]interface {}` | `GEOGRAPHY` | yes |
| GEOMETRY | geometry | `map[string]any` | `map[string]interface {}` | `GEOMETRY` | yes |
| VECTOR | vector | `dbimp.Vector[float64]` | `dbimp.Vector[float64]` | `VECTOR` | yes |
<!-- /dbimp:types -->

The Go types of the kinds follow [TYPES.md](TYPES.md). Three mappings are
proposals that step 9 must settle:

- A `fixed` value of a large scale or precision has no `int64`, so the kind
  decimal gives `*apd.Decimal` (D33). A column of `NUMBER(38,0)` can hold a
  value that does not fit an `int64`, so the Go type depends on the column and
  not on the value.
- A `geography` and a `geometry` arrive as GeoJSON text, and the proposal
  decodes it to `map[string]any`, as the kind geometry allows. A caller that
  wants the text gets a `string` only through a cast in the statement.
- A `vector` names no element type, so the driver cannot tell `FLOAT` from
  `INT` before it reads the values. The proposal reads each as a `float64`.

## Parameters

- A statement takes positional `?` placeholders and named or numbered `:1`
  and `:a` placeholders. The values are in the member `bindings`, an object
  whose keys are the numbers `"1"`, `"2"` or the names, and each value is
  `{"type": <type>, "value": <text>}` (recorded: "a binding of the type
  FIXED", "a numbered placeholder used twice", "a named placeholder").
- The types that bind are `FIXED`, `REAL`, `TEXT`, `BOOLEAN`, `DATE`, `TIME`,
  `TIMESTAMP_NTZ`, `TIMESTAMP_LTZ`, `TIMESTAMP_TZ` and `BINARY`. The value is
  always a string.
- `DATE` takes the milliseconds since the epoch, such as `1791504000000`, which
  gave day `20735` (recorded: "a binding of the type DATE"). The days that a
  `date` column returns do not work as a binding: `20735` gave day 0 (measured
  with `curl`, 2026-10-09).
- `TIME` takes the nanoseconds since midnight, and `TIMESTAMP_NTZ` and
  `TIMESTAMP_LTZ` take the nanoseconds since the epoch. `TIMESTAMP_TZ` takes
  `<nanoseconds> <offset>` with the offset as the column writes it (recorded:
  "a binding of the type TIMESTAMP_TZ"). `BINARY` takes hex text.
- A NULL is `"value": null` with a type (recorded: "a binding of NULL").
- `VARIANT` is not a type of a binding: HTTP 422, `Unsupported data type
  'VARIANT'`, code `002040` (recorded: "a binding of the type VARIANT").
- A value that does not fit its type is HTTP 422, code `002086` (recorded: "a
  binding that does not fit its type"). A placeholder with no binding is HTTP
  422, code `002049` (recorded: "a binding that is not set"). A binding with
  no placeholder is ignored (recorded: "more bindings than placeholders").
- A value of the binding can be an array, which runs the statement once for
  each element. `INSERT` with arrays of three values inserted three rows, and
  the answer counts them (recorded: "a batch insert with arrays of bindings").
  Arrays of different lengths are HTTP 422, code `002099` (recorded: "a batch
  insert with arrays of different lengths").

## Transactions

- `BEGIN` alone and `COMMIT` alone are refused: HTTP 422, code `391911`,
  `Command not supported by SQL API: TRANSACTION_BEGIN` (recorded: "BEGIN
  alone" and "COMMIT alone").
- A transaction is possible inside one request that holds several statements
  with `MULTI_STATEMENT_COUNT`: `BEGIN; INSERT ...; COMMIT` committed, and
  `BEGIN; INSERT ...; ROLLBACK` left no row (recorded: "a transaction in one
  request", "a transaction that rolls back" and "the rows after the
  transaction").
- Each request is its own session. `CURRENT_TRANSACTION()` is NULL in the next
  request, and `ALTER SESSION SET TIMEZONE` did not change the next request
  (recorded: "the transaction of the session", "a change of the session" and
  "the session after the change"). There is no session to hold an open
  transaction across requests.
- So a driver has no `BeginTx` across several statements, and the proposal is
  `dbimp.ErrNotSupported` (D20).

## Errors

- A failed statement answers HTTP 422 with `code`, `sqlState` and `message`:
  a syntax error is `001003` and `42000`, a missing object is `002003` and
  `42S02`, a division by zero is `100051` and `22012`, and a failed conversion
  is `100038` (recorded: "a syntax error", "a missing table", "a division by
  zero", "a conversion error").
- The error comes before any row. A statement that fails halfway produced no
  partition, and the first answer is the error (recorded: "an error after some
  rows were produced"). A later partition can still fail when it is fetched,
  but that was not produced here.
- A statement that reaches its timeout answers HTTP 408, code `000630`
  (recorded: "a statement that reaches its timeout").
- A body with no statement is HTTP 400, code `000900`, `Empty SQL statement`
  (recorded: "a statement with no text"). An unknown handle is HTTP 400, code
  `000709` (recorded: "an unknown handle").
- The message of an error can name the account locator, as `Insufficient
  privileges to operate on account '<locator>'` does (recorded: "a user that
  the role cannot create"). Every recorded file replaces it.
- A wrong token is HTTP 401, code `390144` (recorded: "a wrong token"). A body
  with no token is HTTP 400, code `390146`.
- A role that the user does not hold is HTTP 400, code `390186` (recorded: "a
  role that the user does not hold"). A warehouse or a database that does not
  exist did not fail a statement that needs neither (recorded: "a warehouse
  that does not exist" and "a database that does not exist").
- The error that says the request did not reach the server is not a case here.
  A refusal by the server comes with a code, and a network failure comes with
  none.

## Cancellation and timeouts

- A statement started with `?async=true` answers HTTP 202 at once, with the
  code `333334`, `statementHandle` and `statementStatusUrl` (recorded: "an
  asynchronous statement"). `GET` on the handle answers HTTP 202 while it runs
  (recorded: "the status while it runs") and HTTP 200 with the result when it
  has finished (recorded: "the status when it has finished").
- A statement without `async` waits up to its `timeout`, and then answers
  HTTP 408. It was canceled on the server (recorded: "a statement that reaches
  its timeout").
- `POST /api/v2/statements/<handle>/cancel` answered HTTP 200 for a running
  statement. The statement then answers HTTP 422, code `000604`, `SQL
  execution canceled` (recorded: "cancel the statement" and "the status after
  the cancel"). The same call on a statement that is gone answered the same
  (recorded: "cancel a statement that is gone").
- When the client leaves, the statement runs on. A synchronous statement has
  no handle that the client holds when it leaves, so the proposal is to start
  each statement asynchronously and poll, so that the client always holds the
  handle for a cancel. This is not measured.
- A suspended warehouse resumes at the first statement that needs it, and the
  statement then takes longer. The warehouse was running during this work, so
  the delay is not measured, source: Gemini.

## Statements

- Several statements in one request need `MULTI_STATEMENT_COUNT`. With no
  count, the server counts one, and two statements are refused: HTTP 422, code
  `000008`, `Actual statement count 2 did not match the desired statement count
  1` (recorded: "two statements and the count one"). With the count 2 or 0
  the statements ran, and the answer holds one row, `Multiple statements
  executed successfully.`, and a list `statementHandles` of the child handles
  (recorded: "two statements and the count two", "two statements and the count
  zero").
- A child handle is read with `GET /api/v2/statements/<child>` and gives the
  result of that statement (recorded: "a multi statement with child handles",
  "the first child handle", "the second child handle").
- A comment before or after a statement is accepted (recorded: "a statement
  with a comment"). A final semicolon is accepted (recorded: "a statement
  with a trailing semicolon").
- The answer of `INSERT`, `UPDATE`, `DELETE` and `MERGE` has a member `stats`
  with `numRowsInserted`, `numRowsUpdated`, `numRowsDeleted` and
  `numDmlDuplicates`, and a row that holds the same count (recorded: "a DML
  statement and its counts", "an update and its counts", "a delete and its
  counts", "a merge and its counts"). So `RowsAffected` is known.
- The answer of DDL is one row with a `status` text, such as `Table
  DBIMP_IT_DML successfully created.` (recorded: "a create table and its
  answer"). `TRUNCATE` reports the rows that it deleted in `stats` (recorded:
  "a truncate").
- `SHOW` and `DESCRIBE` answer as a query (recorded: "show the tables", "describe
  the table of types").
- The keys `PRIMARY KEY`, `UNIQUE` and `REFERENCES` are accepted and not
  enforced: two equal rows were inserted (recorded: "create a table with keys
  and a default" and "are the keys enforced"). A default value applies
  (recorded: "the default value").
- `CREATE INDEX` is refused: `Unsupported feature 'SECONDARY INDEX'`, code
  `000002` (recorded: "an index"). A materialized view is refused on this
  account: `Unsupported feature 'MATERIALIZED VIEWS'` (recorded: "a materialized
  view"), which depends on the edition.
- On 2026-10-09 the role of this login could not create a view, a sequence, a
  stream or a dynamic table (HTTP 422, code `003001`, `Insufficient
  privileges`), and the schema `PUBLIC` was dropped afterwards. On 2026-10-10
  `dbsetup` gave the role its own schema, `DBIMP`, with those privileges, and
  all four worked there. A view and a dynamic table returned their rows, a
  sequence gave `1` and `2`, and a stream on a table with change tracking
  returned no row, because no change came after it (recorded: "a view", "the rows
  of the view", "a sequence", "the next value of a sequence", "a stream", "the
  rows of the stream", "a dynamic table", "the rows of the dynamic table").
  These eight requests ran with the schema `DBIMP`, and all the others ran with
  `PUBLIC`.
- `COPY INTO` a table from a stage ran with no file to load (recorded: "copy into
  a table from a stage"). `INSERT OVERWRITE`, a multi table insert, a clustering
  key, a clone, time travel, `FLATTEN`, `QUALIFY` and a path in a variant all
  ran (recorded: "insert overwrite", "a multi table insert", "a clustering
  key", "a clone", "time travel", "flatten", "qualify", "a path in a variant").

### The refused credential (D197)

`errors.Is(err, dbimp.ErrAuthentication)` is true for an `*Error` whose `Code`
is `390144`, or whose `HTTPStatus` is 401 (`Error.Is` in
`snowflake/errors.go`). The recording of "a wrong token" has both: HTTP 401,
code `390144` and the text `JWT token is invalid`.

A statement that the role cannot run does not match. The server sends HTTP 422
and code `002003`, with the text `does not exist or not authorized`, for an
object or a schema that the role cannot see (recorded: "a missing table" and "a schema that
the role cannot read"). A syntax error, an empty statement and
HTTP 400 do not match either. The code `390146`, for a body with no token, is
HTTP 400 and does not match, because no recording shows it for a credential
that the server read.

## Principals

- This login is one user with one role. It can use the warehouse and the
  database, and create tables in the schema `PUBLIC`. It cannot read
  `SNOWFLAKE.ACCOUNT_USAGE`, cannot create a user, and cannot use the role
  `ACCOUNTADMIN` (recorded: "a schema that the role cannot read", "a user that
  the role cannot create", "a role that the user does not hold").
- The grants of the role are in the recording (recorded: "the grants of the
  role").
- There is no ordinary user besides this login, so the principals of the
  recording are one.
- The version is `SELECT CURRENT_VERSION()` for this role (recorded: "the
  version").

## Flavors

Snowflake is one product. The edition (standard, enterprise) changes what
works: a materialized view is refused on this account. The edition is not read
from the server in this work.

## Interfaces

`snowflake/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and the token that the driver signs with the private key of the DSN (D183). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the token, the login and the warehouse, and costs little. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because each request is its own session (measured). |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver binds with a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.LocalDateTime, and a list and a map that fail with dbimp.ErrArguments (D183). |
| `driver.QueryerContext` | yes | The statement goes to POST /api/v2/statements with its arguments as typed bindings, which the server binds to its ? (D183). |
| `driver.ExecerContext` | yes | Exec reads the result to its end with no decoding, and RowsAffected is the sum of the counts in stats, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the server refuses BEGIN alone and each request is its own session (D183). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result. The driver reads the partitions of one result as one set of rows. |
| `driver.RowsColumnTypeScanType` | yes | The metadata names the type of each column, with its precision and its scale, and each type has one Go type (D135 and D183). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of the column in upper case, such as FIXED. The server names a geography and a geometry object, so both report OBJECT (measured). |
| `driver.RowsColumnTypeLength` | yes | A text column and a binary column have the length that the metadata names. |
| `driver.RowsColumnTypeNullable` | yes | The metadata has the member nullable for each column. |
| `driver.RowsColumnTypePrecisionScale` | yes | A fixed column has its precision and its scale in the metadata. |
<!-- /dbimp:interfaces -->

The facts that the table needs are in the sections above. `Ping` runs
`SELECT 1`. The request is a statement, so it checks the token, the login and
the warehouse, and it costs the server little. A connection holds no state on
the server, because each request is its own session (recorded: "the
transaction of the session" and "the session after the change"), so the driver
has no `SessionResetter`. The driver sends no output-format parameter of the
session, so a value keeps the form that the Types section names.

The rows of a result hold a function that fetches the next partition. The
function holds the context of the statement, because the request for each
partition after the first needs it. The rows have no field of the type
`context.Context`, as the rows of Druid and Drill have none. Question 14 asks
whether rule 4 of `AGENTS.md` names them.

## Faults

- The driver of `usql` is `gosnowflake`, which speaks the private protocol and
  needs Arrow for large results. A driver here reads JSON only.
- The `DATE_OUTPUT_FORMAT` and the other output formats change the wire text of
  a value, so a driver must send none.
- The recorded error messages can hold the account locator. The recorder does
  not know that, so each recording must be searched and redacted.
- The text of an array writes a NULL inside it as `undefined`, which is not
  JSON (recorded: "every type"). A decoder of JSON fails on it, so the driver
  reads each `undefined` that stands outside a string as `null`
  (`snowflake/types.go`).
- The metadata names a geography column and a geometry column `object`
  (recorded: "every type" and "columns of a result with no rows"), and it has no
  other member that tells them apart. So `ColumnTypeDatabaseTypeName` gives
  `OBJECT` for both, and the Go type is the same `map[string]any` (question 7).
- The handle of the statement is in the member `statementHandle`, which the
  server writes after the rows. It is also in the header `Link` of the answer,
  before the rows. The driver takes it from the header first, so that it can
  cancel a statement before it has read the last row (`snowflake/connector.go`).
- A request to cancel a statement that has ended answers HTTP 200 (recorded:
  "cancel a statement that is gone"). The driver does not rely on it. It sends
  no cancel for rows that the caller read to the last row, such as the rows of
  `QueryRow` for a result of one row (question 12).
- The first statement of a request of several statements gives one row and a
  list of child handles. The driver does not read the children, so it refuses
  the parameter `MULTI_STATEMENT_COUNT` (question 8).

## Second opinions

- Gemini (`gemini-3.1-pro-preview`, 2026-10-08) was asked the four questions of
  step 5a and named the operations, the schema features and the types in
  `features.json`. It said that `PUT` and `GET` do not work on the SQL API,
  which is true. It said that the keys are not enforced, which is true. It
  said that there is no index, which is true: `CREATE INDEX` is refused.
- Gemini (2026-10-09) was asked what step 6 missed. Its leads, and what the
  server said:
  - A multi statement answers with child handles, and the driver must `GET`
    each one. True (recorded: "the first child handle").
  - A JWT lasts at most 60 minutes, and the driver must sign a new one. True
    for the lifetime that `dbsetup` used (3300 seconds). The limit of 60 is
    not measured.
  - HTTP 429 and 503 must be retried with backoff. Not measured, because the
    work did not reach a limit.
  - The parameters `TIMEZONE` and `DATE_OUTPUT_FORMAT` go in the body. True,
    and the second one changes a `date` value (recorded: "a date output format
    of the statement").
  - A suspended warehouse delays the first statement. Not measured.
  - Batch inserts use arrays in the bindings. True (recorded: "a batch insert
    with arrays of bindings").
- Gemini (2026-10-09) reviewed the type mapping against TYPES.md. Its answer was
  cut short after three points. It said that a `real` of `NaN`, `inf` or `-inf`
  must be read as `math.NaN()` and `math.Inf()`, which the table already says. It
  said that `vector` is risky, because Snowflake has `VECTOR(INT, N)` as well as
  `VECTOR(FLOAT, N)` and the `rowType` names neither, which the Types section
  says too. It said that the text of a time or a timestamp, with up to nine
  digits of fraction and the offset in minutes plus 1440, needs careful
  parsing. It did not say what `gosnowflake` or the JDBC driver return for each
  type, so that comparison is not made.
- Kimi K3 (`kimi-k3`, 2026-10-09) was asked the four questions of step 5a, as a
  second model, because DeepSeek never answered. It named every operation, schema
  feature, feature and type of the survey except `INSERT OVERWRITE`. It added
  `TRUNCATE`, `CALL`, `PUT` and `GET`, `INSERT FIRST`, `AUTOINCREMENT`, stages,
  pipes, tasks, masking policies and tags, and it said that there is no index
  (the Search Optimization Service takes its place), which is true. It said that
  a `VARCHAR` is at most 128 MB and a `BINARY` at most 64 MB, which is not
  measured. It gave no native interval type, which matches the recording.
- DeepSeek (`deepseek-v4-pro`) timed out on every try, in all, on 2026-10-08
  and 2026-10-09. It gave no answer.
- The Go client was read on 2026-10-08: `gosnowflake` v1.18.1 in the module
  cache. A client in another language was not read, so step 5a is not complete
  for the survey of a second client.

## Open questions

1. R. The account is a trial, and CI would need a secret key and credit. Whether
   a driver here can have integration tests in CI is a question for Ken. The
   alternative is unit tests with the recorded exchanges, which this work made,
   and an integration test that runs only where a person supplies an account.
2. The secret of the DSN. D94 says it is the password of the URL, and the key
   of Snowflake is about 1,600 characters, which a URL can hold, but the user
   is not enough: the driver also needs the user name and the account for the
   claims. The choice is for step 9.
3. Closed. The four schema features that the role could not create (a view, a
   dynamic table, a sequence and a stream) were measured on 2026-10-10 in the
   schema `DBIMP`, and each one works.
4. The statement handle after a client leaves, a rate limit (HTTP 429), the
   delay of a suspended warehouse, the lifetime of a result, a result larger
   than the size that the server keeps, and the other kinds of token are not
   measured.
5. Gemini did not name the cap on the size of the body of a request, and none
   was met here.

The questions below came up while the driver was written. Ken decided questions
6 to 16 on 2026-10-10, and D183 item 15 holds the decisions, except for
question 14, which D183 item 14 holds. Questions 1 to 5 are older, and the work
did not settle them.

6. Decided (D183 item 15). The type `fixed` has two entries in `features.json`
   and two rows in the type table. `FIXED INTEGER` is a column of scale 0 and
   precision 18 or less, with the kind integer and the Go type `int64`.
   `FIXED DECIMAL` is any other `fixed` column, with the kind decimal and the
   Go type `*apd.Decimal`. The matrix of `TYPES.md` shows both.
7. Decided (D183 item 15). A geography column and a geometry column both have
   the type `object` in the metadata, so `ColumnTypeDatabaseTypeName` gives
   `OBJECT` for them. A caller that needs to tell the two types apart must read
   the type from the column of the table. The type table keeps the rows
   `GEOGRAPHY` and `GEOMETRY`, as the survey names them.
8. Decided (D183 item 15). The driver refuses a request of several statements
   with `dbimp.ErrNotSupported` for now, and it refuses the parameter
   `MULTI_STATEMENT_COUNT`. The first version has no transaction.
9. Decided (D183 item 15). The key `timeout` of the DSN is a duration with a
   unit, such as `60s` or `1m`, like the other drivers. A bare number is
   refused with an error that names the form. The driver rounds the value up to
   whole seconds for the member `timeout` of the body, and `WithTimeout` does
   the same.
10. Decided (D183 item 15). A `timestamp_ltz` value has the location of the key
    `timezone` or of `WithTimeZone`, and `time.Local` when none is named. The
    name `Local` in the key is `time.Local`. So with no zone named, the value
    follows the system or the variable `TZ`. For a server, set `timezone=UTC`.
    The session zone of the account is `America/Los_Angeles`, and the driver
    sends a zone only when a caller names one.
11. Decided (D183 item 15). The poll of a statement that runs waits 25 ms, then
    50 ms, 100 ms, 200 ms and 400 ms, and then 500 ms for each poll that
    follows. These numbers are not measured.
12. Decided (D183 item 15). The driver sends no cancel when the caller closes
    the rows after the last row of the last partition, because the statement
    has ended. This is what `QueryRow` does for a result of one row, so it costs
    no second request. The driver sends the cancel in every other early close,
    and when the context ends.
13. Decided (D183 item 15). The account in the claims of the token is the host
    without the suffix `.snowflakecomputing.com`, cut at the first dot, in upper
    case. So `xy12345.us-east-1.snowflakecomputing.com` gives `XY12345`, and
    `myorg-myaccount.privatelink.snowflakecomputing.com` gives
    `MYORG-MYACCOUNT`, as `gosnowflake` does and as the key-pair documentation
    of Snowflake says. Only the form `<org>-<account>` is measured. A DSN with a
    port or an IP address, and no suffix, names a fake server for a test, and
    the account is the whole host in upper case.
14. Decided (D183 item 14). The rows keep the context of the statement in a
    function, and fetch the next partition with it. Rule 4 of `AGENTS.md` names
    the rows of Snowflake, as it names the other rows that fetch pages.
15. Decided (D183 item 15). `WithParameter` refuses the nine parameters that
    change the text of a value (`DATE_OUTPUT_FORMAT`, `TIME_OUTPUT_FORMAT`,
    `TIMESTAMP_OUTPUT_FORMAT`, `TIMESTAMP_LTZ_OUTPUT_FORMAT`,
    `TIMESTAMP_NTZ_OUTPUT_FORMAT`, `TIMESTAMP_TZ_OUTPUT_FORMAT`,
    `BINARY_OUTPUT_FORMAT`, `GEOGRAPHY_OUTPUT_FORMAT` and
    `GEOMETRY_OUTPUT_FORMAT`). Only the first two kinds are recorded to change a
    value, and the list is not measured.
16. Decided (D183 item 15). A value of a type that the driver has no Go type
    for, such as a `map` column, fails the row with `dbimp.ErrNotSupported`
    (D135). A table with such a column cannot be read with `SELECT *`.
17. Three gates of the root package fail for this driver, and the failures come
    from steps 5a and 6, not from the code. `TestEveryDriverHasItsManifest`
    wants `noOrdinaryUser` in `manifest.json`, which the file lacks, though the
    Principals section says that there is one login.
    `TestEveryDriverHasItsFeatures` wants a survey that asked two models, and
    `features.json` names one, because DeepSeek timed out. It also wanted a
    verdict for every entry, and four were `not measured`: they are measured now
    (question 3). The two
    entries `FIXED INTEGER` and `FIXED DECIMAL` replace `FIXED` (question 6). The work
    that wrote the driver did not change these files.

## Integration tests

The integration tests of the driver read `SNOWFLAKE_DSN` and skip when it is
empty (hard rule 9). The variable holds the DSN of D183, with the private key as
the password, and the role and the warehouse as the keys `role` and `warehouse`:

    SNOWFLAKE_DSN='snowflake://USER:KEY@ORG-ACCOUNT.snowflakecomputing.com/DATABASE/SCHEMA?role=ROLE&warehouse=WH'

The `url` that `dbrun` prints names the key as the query key `privateKey`, so a
person moves it into the password before the run. `dbrun` does not start
Snowflake, and the workflow has no job for it and no secret (D183 item 13). A
person runs the tests on an account with the login that `dbsetup` made:

    go test -race -count=1 -run Integration -v ./snowflake/...

The login is one user with one role, so each test runs as that user only, and
the manifest has no ordinary user. The tests make their tables in the database
and the schema of the DSN, with the name of the run as the prefix of each table
(`DBIMP_IT_` and eight characters). `TestMain` looks for a table of the run that
a test left, drops it and fails. The session zone is UTC for every test.

The tests were written on 2026-10-09 with no account, and they first ran on
2026-10-10, as the login `<user>` in the schema `DBIMP`. The first run passed
six of the nine tests and found five faults of the driver and of its tests,
which are fixed (see "What the first live run found"). The last full run on
2026-10-10 passed all nine tests: 47 subtests, with the round trip of every
type, in about six minutes. The tests and what each one holds:

- `TestIntegrationConnect`: the login, `Ping`, the version, and the role, the
  warehouse, the database and the schema of a statement.
- `TestIntegrationErrors`: an error of the server before any row, a statement
  that fails halfway, a statement that reaches its timeout, two statements with
  no count, and a role that the login does not hold.
- `TestIntegrationTransactions`: `BeginTx` and the refusal of `BEGIN`.
- `TestIntegrationContext`: a deadline that ends while a statement runs.
- `TestIntegrationBindings`: each Go type that the driver binds.
- `TestIntegrationCRUD`, `TestIntegrationSchema` and `TestIntegrationFeatures`:
  the entries of `features.json`, in the order that the file names them. The
  tests of the objects that the role cannot create skip with that reason
  (question 3).
- `TestIntegrationRoundTrip`: every type that `features.json` marks yes, with
  `dbimptest.RoundTrip`, as a bound argument and as a literal. A `FIXED` column
  has three tables, for an integer, a decimal and a wide integer, because the Go
  type depends on the column. A variant, an object, an array, a geography, a
  geometry and a vector go in as JSON text that the statement parses, because
  they have no Go type to bind.

A vector of a `FLOAT` has 32 bits and writes six digits after the point, so the
values of the test are exact in that form.

### What the first live run found

These are facts of the service and faults of the first version, measured on
2026-10-10 with probes and the tests:

- A statement that fails while it runs, such as `SELECT 1/0`, answers HTTP 422
  with a code and a message only, with no SQLSTATE and no handle, when it ran
  with `?async=true`. A statement that fails to compile still answers with the
  SQLSTATE and the handle. The driver sets the handle from the one that it holds,
  and the SQLSTATE of a run-time error is empty.
- The row of the answer of `UPDATE`, `DELETE` and `INSERT` names the count of the
  rows: `number of rows updated` and `number of multi-joined rows updated` for an
  update, `number of rows deleted` for a delete. The member `stats` is absent when
  no row changed, so an update of no row has no stats. `INSERT OVERWRITE` has
  stats of 1 inserted and 3 deleted for a table of three rows, and its row says
  1. The driver reads the count from the row, without the multi-joined column,
  and from `stats` when the row holds none. `TRUNCATE` of a table with rows has
  stats of the rows deleted, and `TRUNCATE` of an empty table has none, so the
  count of the empty one is unknown.
- A binding of the type `REAL` takes `NaN`, `Infinity` and `-Infinity`. It
  refuses `inf`, `-inf`, `INF` and `+inf` with HTTP 422, code `002086`, `Invalid
  bind value`. A number too large, such as `1e999`, is read as infinity.
- A `TIMESTAMP_TZ` binding does not convert by itself into a `TIMESTAMP_LTZ`
  column in a list of values: HTTP 422, code `002023`, `Expression type does not
  match column data type`. A `TIMESTAMP_LTZ` binding, or a `CAST(? AS
  TIMESTAMP_LTZ)` in the statement, works. The driver still binds a `time.Time`
  as `TIMESTAMP_TZ` (D183 item 6), and the test casts.
- The pipe operator is accepted by the SQL API: `SHOW PRIMARY KEYS IN DATABASE
  ->> SELECT ... FROM $1` ran through the driver (reported by `dbmeta`,
  2026-10-10). A bind parameter after the pipe is refused by the server. `USE` is
  refused (`391911`, `Command not supported by SQL API: USE`), so each statement
  runs in its own session and a `TEMPORARY` table does not last between
  statements. A login with no database in the path of the DSN gets `391918
  (22000)`, which asks for the database in the body or in the parameter
  `DATABASE`, where `gosnowflake` gave `090105`.
- A cleanup that runs in `t.Cleanup` must not use `t.Context()`, which has ended
  by then: the first version left a view, a sequence, a stream and a dynamic table
  in the schema, and the leftovers were dropped by hand. The tests use
  `context.WithoutCancel(t.Context())`.

The replay tests run with no account. They answer from the recorded exchanges,
and `TestReplayEveryType` reads the exchange "every type" through the real
decoder (step 14a). The other tests of the package use fake servers that the
tests start, and a key that `crypto/rsa` makes for each run.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-09 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of Snowflake from the sections above.

### The server

| | Couchbase | Snowflake |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /api/v2/statements?async=true`, with `statement`, `timeout`, `warehouse`, `role`, `database`, `schema`, `parameters` and `bindings` (Requests) |
| Database | The key `query_context` of the body | The members `database` and `schema` of the body (Requests) |
| Language | SQL++, which is close to SQL | The SQL of Snowflake, one statement for each request. Several need `MULTI_STATEMENT_COUNT` (Statements) |
| DDL | In SQL++ | In SQL. A key is declared and not enforced, and an index is refused (Statements) |
| Parameters | `?`, `$1` and `$name` | `?`, `:1` and `:name`, with a typed binding for each in `bindings` (Parameters) |
| Framing | One body for the whole result, which does not page | One JSON object. The metadata comes first, then the rows of the first partition in `data`, then the other members. Each later partition is another request, `GET` with `partition=N` (Responses) |
| Columns | `signature`, before the first row | `rowType` in `resultSetMetaData`, before the first row, with the type, the precision and the scale of each column (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement (Responses) |
| Errors | Can come with HTTP 200, after some rows | Before any row, with HTTP 400, 401, 408 or 422 and a JSON object of the code, the SQLSTATE and the message. A later partition can fail (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, and every value is a string or null, with the type name of the column. The text of a date is a day number, and the text of a timestamp is seconds with a fraction (Types) |
| Cancel | The server stops a query when the client leaves | A statement runs on when the client leaves, and `POST /api/v2/statements/<handle>/cancel` stops it. The driver starts every statement with `async=true`, so that it always holds the handle (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None across requests. `BEGIN` alone is refused, and each request is its own session (Transactions) |
| Authentication | Basic, or `creds` in the body | A JWT that the client signs with an RSA key, as a Bearer token (Requests) |
| Default port | 8093, or 18093 with TLS | 443, with TLS always |

The differences that a caller sees:

- A result can have several partitions. The driver reads the first from the answer
  and each later one only when the caller has read the rows before it, and an
  error in a later partition wraps `dbimp.ErrIncomplete` (D183 item 8).
- Every statement is started with `async=true` and polled until it ends, so a
  statement that takes a long time costs a request each interval, and a cancel
  needs no request that the caller makes (D183 item 9).
- The password of the DSN is a private key, and the driver sends a token that it
  signs, never the key (D183 items 2 and 3).
- A statement of several statements, and a transaction, have no form (D183 item 7
  and question 8).
- An integer of up to 18 digits is an `int64`, and any other `fixed` is an
  `*apd.Decimal`, so the Go type of a number depends on its column (D183 item 4).
- A date, a time and a timestamp keep their types, and a timestamp with a zone
  keeps its offset (D183 item 4).

### The driver

| | `couchbase` | `snowflake` |
| --- | --- | --- |
| Size, without tests, on 2026-10-09 | About 1300 lines in 8 files | About 2400 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `User`, `Password` (the private key), `Database`, `Schema`, `Role`, `Warehouse`, `TimeZone` and `Timeout`. The DSN has the keys `role`, `warehouse`, `timeout` and `timezone` (D183) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase`, `WithSchema`, `WithRole`, `WithWarehouse` and `WithTimeZone`, through `WithOptions` or an argument (D109). `WithParameter` sets a parameter of the session in `parameters`, and refuses ten (questions 8 and 15). `WithReadonly(true)` fails with `dbimp.ErrNotSupported` |
| Arguments | Sent to the server as `args` and `$name` | Typed bindings, from the Go type of each argument: `FIXED`, `REAL`, `TEXT`, `BOOLEAN`, `BINARY`, `DATE`, `TIME`, `TIMESTAMP_NTZ` and `TIMESTAMP_TZ`. A list and a map fail with `dbimp.ErrArguments` (`snowflake/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own, which reads the metadata, then one row for each call, then the members after the rows, and then the next partition (`snowflake/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypeLength` for a text and a binary column, and `ColumnTypePrecisionScale` for a `fixed` column |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `int64` or `*apd.Decimal`, `float64`, `string`, `[]byte`, `bool`, `dbimp.Date`, `dbimp.LocalTime`, `dbimp.LocalDateTime`, `time.Time`, the decoded JSON value, `map[string]any`, `[]any` and `dbimp.Vector[float64]` (D135 and D183) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `stats`, or `dbimp.ErrNotSupported` when the answer has none. `LastInsertId` always gives `dbimp.ErrNotSupported` (D178 item 14) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D183 item 7) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | When the context ends before the driver read the whole result, and when the caller closes the rows before the last row, the driver sends the cancel by the handle, with a limit of 5 seconds (D183 item 9 and question 12) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, SQLState, Message, Handle}`, which unwraps to `*dbimp.StatusError`, and the sentinels `ErrCut`, `ErrCanceled` and `ErrTimeout` |
| Authentication | Basic | A JWT of the key pair, signed in the package with RS256 and renewed five minutes before its end. The driver follows no redirect, so the token goes to the host of the DSN only (D183 items 3 and 10) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, the three sentinels, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a date, a time, a decimal and a vector too, where
  Couchbase gives JSON shapes (D135 and D183).
- The Go type of a `fixed` column depends on its precision and its scale
  (D183 item 4 and question 6).
- `RowsAffected` gives an error for DDL, and the count for a statement that
  changes rows, as the sum of the three counts (D178 item 14).
- The driver sends one request to start a statement, one request for each poll,
  and one request for each later partition, where Couchbase sends one (D183
  items 8 and 9).
- `WithParameter` adds a parameter of the session, where it replaces a key of the
  body in Couchbase, and it refuses the parameters that change the text of a
  value (question 15).
- A DSN needs a private key, a user and a host that ends in
  `.snowflakecomputing.com` (D183 item 2 and question 13).
- The key `timeout` is a duration with a unit, such as `60s`, and a bare number
  is refused (question 9).
- A `timestamp_ltz` value has the location of the key `timezone`, and
  `time.Local` when none is named. A server sets `timezone=UTC` (question 10).

