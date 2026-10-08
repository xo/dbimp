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
  `<org>-<account>`. The lifetime is at most one hour, and `dbsetup` used 3300
  seconds. The form with the account locator did not work for the login
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
- `boolean` is `true` or `false`.
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
| fixed, scale 0, precision 18 or less | integer | `int64` | `int64` | `FIXED` | yes |
| fixed, any other | decimal | `*apd.Decimal` | `*apd.Decimal` | `FIXED` | yes |
| real | float | `float64` | `float64` | `REAL` | yes |
| text | string | `string` | `string` | `TEXT` | yes |
| binary | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| boolean | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| date | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| time | time of day | `dbimp.LocalTime` | `dbimp.LocalTime` | `TIME` | yes |
| timestamp_ntz | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP_NTZ` | yes |
| timestamp_ltz | timestamp | `time.Time` | `time.Time` | `TIMESTAMP_LTZ` | yes |
| timestamp_tz | timestamp | `time.Time` | `time.Time` | `TIMESTAMP_TZ` | yes |
| variant | json | `the decoded JSON value` | `interface {}` | `VARIANT` | yes |
| object | map | `map[string]any` | `map[string]interface {}` | `OBJECT` | yes |
| array | array | `[]any` | `[]interface {}` | `ARRAY` | yes |
| geography | geometry | `map[string]any` | `map[string]interface {}` | `GEOGRAPHY` | yes |
| geometry | geometry | `map[string]any` | `map[string]interface {}` | `GEOMETRY` | yes |
| vector | vector | `dbimp.Vector[float64]` | `dbimp.Vector[float64]` | `VECTOR` | yes |
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
- The role of this login cannot create a view, a sequence, a stream or a
  dynamic table (HTTP 422, code `003001`, `Insufficient privileges`), so those
  four are not measured (recorded: "a view", "a sequence", "a stream", "a
  dynamic table"). The refusal is of the role, not of the product.
- `COPY INTO` a table from a stage ran with no file to load (recorded: "copy into
  a table from a stage"). `INSERT OVERWRITE`, a multi table insert, a clustering
  key, a clone, time travel, `FLATTEN`, `QUALIFY` and a path in a variant all
  ran (recorded: "insert overwrite", "a multi table insert", "a clustering
  key", "a clone", "time travel", "flatten", "qualify", "a path in a variant").

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

Not written yet. It is the table of step 10, which a driver generates.

## Faults

- The driver of `usql` is `gosnowflake`, which speaks the private protocol and
  needs Arrow for large results. A driver here reads JSON only.
- The `DATE_OUTPUT_FORMAT` and the other output formats change the wire text of
  a value, so a driver must send none.
- The recorded error messages can hold the account locator. The recorder does
  not know that, so each recording must be searched and redacted.

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
3. Four schema features are not measured (a view, a dynamic table, a sequence
   and a stream), because the role cannot create them. A role with those
   privileges would settle them.
4. The statement handle after a client leaves, a rate limit (HTTP 429), the
   delay of a suspended warehouse, the lifetime of a result, a result larger
   than the size that the server keeps, and the other kinds of token are not
   measured.
5. Gemini did not name the cap on the size of the body of a request, and none
   was met here.
