# Exasol

This file holds what is known about Exasol over its WebSocket API, for the driver
`exasol` (W44, D200 and D203 to D215). The headings are the template of [DRIVER.md](DRIVER.md). A fact is
"recorded", with the name of its request in quotes, "measured", with how and when, or "not
measured", with its source.

A recorded fact has its request in `testdata/exasol/requests.json` under that name, and the
exchange in the file that `testdata/exasol/manifest.json` names. All recordings are of the
release `exasol-2026.2.0` (the build `2026.2.0-nano.6`), which `dbrun` started on 2026-10-11,
and of the user `sys`. The recorder dials with the WebSocket client of the root package
(D200) and writes one file for each request frame and its reply frame ([DESIGN.md](DESIGN.md),
"WebSocket exchanges"). The password of a login is RSA-encrypted with a new key each time, so
the recorder writes it as `REDACTED`.

## Summary

Step 2 measured this on 2026-10-11 against `exasol-2026.2.0`, which `dbrun` started as
`nano.6` on a random port. The probe was a throwaway program that used the WebSocket
client of the root package (D200). Steps 3 to 7 then recorded the rest, as the sections
below name. `exasol-driver-go` v1.1.1 is the second source, read in place and never
imported (D29). The documents of the WebSocket API at `github.com/exasol/websocket-api` (the
protocols v1 to v5) are the third source, and the measurements agree with them except where a
section says so. The `dbrun` name of the release is `exasol-2026.2.0`, and its entry in `dbmeta` is
`container/exasol.go`. The entry has one older release, the Community Edition 2025.2.1, which
is a virtual machine and which this work did not use.

### R, H and S

- R passes. `dbrun start exasol-2026.2.0` started one container. It listens on
  port 8563 inside, and the user is `sys`.
- H passes with a change in meaning, which D200 item 1 allows. The query goes in
  a JSON object, in one text frame of a WebSocket, and the answer is one JSON text
  frame. No frame is binary unless the client turns on compression. See "Responses".
- S passes. The query is Exasol SQL, and a person can type it.

### The conditions of "When it cannot be a driver"

- The columns come before the first row. Every result set holds `columns`, with the
  name and the type of each column, in the order of the statement, even for a result with no
  row in the first message. Passes (recorded: "columns out of order", "two columns with one
  name", "select 1000 rows").
- The result can be read in parts. A result set of 1000 rows or more has a
  `resultSetHandle`, `numRows` for the whole result and `numRowsInMessage` of 0. The `fetch` command
  reads from `startPosition`, and `numBytes` bounds the size of one answer. Memory is bounded by
  `numBytes`, as long as the client chooses it. Passes (recorded: "select 3000 rows", "fetch
  the first page of 8000 bytes").
- The server does not cut a result short. A fetch at the position `numRows` is an error
  (`Invalid result set seek`), and a fetch after `closeResultSet` is an error
  (`Resultset handle 33 is unknown`). Recorded: "fetch at the end", "fetch after the close". Not
  measured: how long an open handle lives, and the limit of open handles in a session.
- No package is needed. The login encrypts the password with RSA PKCS #1 version 1.5, and
  `crypto/rsa` does it. Compression is `compress/zlib`. Passes.

So Exasol can be a driver, and the client of the WebSocket is the only new
code that the root package needs.

### The verdict

Exasol meets R, H and S, and it meets no condition of "When it cannot be a driver".
It can be a driver.

### The risks

- H is a WebSocket, not plain HTTP. D200 allows it, and D202 item 1 accepted the scope.
- The client reads a whole message and then decodes it. A message is at most
  `maxDataMessageSize`, 67108864 bytes, and `numBytes` of `fetch` keeps it smaller. The first
  result of an `execute` is bounded by the same size (see "Responses").
- One request is in flight on a session. A cancel from another goroutine needs a second
  frame on the same socket, so the driver must serialize its writes and read the answer of the
  aborted query, never an answer of the abort, which has none (see "Cancellation and timeouts").
- The server ends a session whose connection is idle for longer than its idle timeout, with no
  close frame (see "Cancellation and timeouts").
- Not measured: a ping from the client (the documents say that the server answers it with a pong
  and never sends one), the heartbeat of `feedbackInterval`, the maximum number of sessions, and
  the local import and export through a proxy connection, which the server refuses without one.

## Requests

All of this is recorded unless the line says "measured" or "not measured".

- The connection. `wss://host:8563`. The server uses TLS with a certificate that nothing
  trusts, so a client that checks it needs `validateservercertificate=0`, as `dbmeta` writes
  it. The path of the URL is `/`. A plain `ws://` upgrade succeeds, but the login then answers
  `Only TLS connections are allowed.` with the code `08004` (measured in step 2, 2026-10-11).
  The server chose no extension and no subprotocol, and the client offered none.
- Each request is one JSON object in one text frame, with the member `command`. Each answer is
  one frame: `{"status":"ok","responseData":{...}}` or `{"status":"error","exception":{"text":...,
  "sqlCode":...}}`. An answer can also hold `attributes`, the session attributes that the
  statement changed, such as `currentSchema` ("create the schema") or `timezone` ("set the time
  zone to Tokyo").
- The login has two messages. The first is `{"command":"login","protocolVersion":3}`. Its answer
  holds `publicKeyPem`, `publicKeyModulus` and `publicKeyExponent` (both in hex). The second has
  no `command`. It holds `username`, `password` (the password encrypted with that key, in Base64),
  `useCompression`, `clientName`, `driverName` and `attributes` ("login with protocol 3: ask for
  the key", "login with protocol 3: send the credentials"). The answer holds `sessionId`,
  `releaseVersion` (`2026.2.0-nano.6`), `maxDataMessageSize` (67108864), `maxIdentifierLength`
  (128), `maxVarcharLength` (2000000), `identifierQuoteString` (`"`), `timeZone` and
  `timeZoneBehavior`. The `sessionId` is a number of 19 digits, so it is larger than 2^53 and a
  client must read it as an integer and not as a `float64`.
- The protocol version. The server answered with the version that was asked for when it was 3, 4
  or 5 ("login with protocol 3: send the credentials", "login with protocol 4: send the credentials",
  "login with protocol 5: send the credentials"). Measured on 2026-10-11 with the probe, and not
  recorded: a login that asked for 9 was answered with 3. The two recordings "login with an unknown
  protocol" and "login with protocol 1" hold only the answer with the key, and their credentials were not
  sent, so the version of an old protocol is not measured. A login with no `protocolVersion` is an error
  with the code `00000` ("login with no protocol"). The documents say that version 5 needs
  Exasol 8.32.0 or later.
- A query. `{"command":"execute","sqlText":"..."}` returns `results`, a list with one
  entry for each statement, each `resultSet` or `rowCount` ("insert one row", "select the rows").
  `executeBatch` takes `sqlTexts` ("two statements in one batch").
- Other commands that the recordings use: `createPreparedStatement`, `executePreparedStatement`,
  `closePreparedStatement`, `fetch`, `closeResultSet`, `setAttributes`, `getAttributes`,
  `getHosts` (it needs `hostIp`), `abortQuery`, `disconnect` and `loginToken`. The metadata
  commands `getTypeInfo`, `getSchemas`, `getTables`, `getPrimaryKeys` and `getTableTypes`
  answer with a result set and no handle ("metadata command: getTypeInfo" and the three that
  follow it in the script). The documents list more, such as `getColumns`, `getKeywords`,
  `getUsers`, `getOffset`, `getResultSetHeader`, `enterParallel` and `requestParallelConnections`.
- A command that the server does not know is an error with the code `00000`
  (`Invalid command: noSuchCommand`, "an unknown command", "lead: a logout command"). A frame with no
  `command` is the same error ("a frame with no command", "a frame that is a JSON array", "a
  frame that is not JSON").
- A message that is not a login is refused before the login. `execute` before the login answers
  `Invalid login request command: execute`, and the credentials with no earlier `login` answer
  `JSON value '/command' is required, but was not provided.` ("execute before the login", "send
  the credentials with no key"). A second `login` after the login answers `Invalid command:
  login` ("login a second time on the same session").
- A WebSocket ping. Not measured. The root client has no ping, and the documents say that the
  server answers a ping with a pong and never sends a ping.

## The DSN

What `dburl` writes, the old driver reads and a new driver needs. The choice of the form is a
decision of step 9 (D27 and D35).

- `dburl` has the scheme `exasol`, with the aliases `ex` and `exa`, the generator `GenExasol`,
  the dialect `exasol`, and the `GoPackage` `github.com/exasol/exasol-driver-go`. The generator
  turns `exasol://user:pass@host:port/schema?key=value` into `exa:host:port;schema=...;user=...;
  password=...;key=value`. The host defaults to `localhost` and the port to `8563`. The path is
  the schema. (Read in `dburl/dsn.go`, 2026-10-11.)
- `dbmeta` writes `exa:127.0.0.1:PORT;user=sys;password=exasol;validateservercertificate=0` and
  the URL `exasol://sys:exasol@127.0.0.1:PORT?validateservercertificate=0`, with the password
  `exasol` and no database name (`dbrun dsn --json exasol-2026.2.0`, 2026-10-11).
- `exasol-driver-go` v1.1.1 reads `exa:<host>[,<host_n>]...:<port>[;<key>=<value>]...`. A host
  can be a range such as `exasol1..3`, and the driver shuffles the hosts and tries each one. A `;`
  in a value is written `\;`. Its keys are `user`, `password`, `accesstoken`, `refreshtoken`,
  `autocommit` (default 1), `clientname` (default `Go client`), `clientversion`, `compression`
  (default 0), `encryption` (default 1), `validateservercertificate` (default 1),
  `certificatefingerprint` (the SHA-256 of the certificate), `fetchsize` (in kB, default 128 x
  1024 in the README and 2000 in the comment of the builder, so the two disagree), `querytimeout`
  (seconds), `resultsetmaxrows`, `schema`, `urlpath` and `localimportencryption` (deprecated).
  (Read in `pkg/dsn/dsn.go` and its README, 2026-10-11.)
- A login needs a user and a password, or an access token or a refresh token (the command
  `loginToken`). A `loginToken` answered `{"status":"ok"}` and a wrong token then answered
  `Connection exception - authentication failed` with `08004` ("login with a token that is
  wrong", "send the wrong token"). A good token was not measured.
- `{{password}}` never goes in a recording. The password of a login is encrypted with the key
  of the session, and the recorder redacts it.
- The `sys` password of the container is `exasol`. The server needs `validateservercertificate=0`
  or a fingerprint, because its certificate is for `localhost` and no authority signed it
  (the `dbmeta` entry).

## Responses

- The framing. One request frame, one answer frame. A frame is text, and it is zlib data in a
  binary frame once compression is on. Nothing streams. A whole result that is not large is in
  the answer, and a large one has a handle. A result that is a list of rows is not a list: `data`
  is a list of columns, and each column is the list of the values of that column ("select every
  row of types"). So the driver reads a message with the root client, decodes the columns and
  hands out one row at a time.
- The columns. `columns` is a list of `{"name":..., "dataType":{...}}` in the order of the
  statement. The names are the names of the statement in upper case, or the text of the
  expression when it has no alias (`"1"`, `"COUNT(*)"`, `"Parameter()"`), and a quoted alias keeps its case.
  Two columns can have one name ("two columns with one name"). Recorded: "columns out of order", "a
  column with no name", "lower case name".
- The data type. The member `type` names the type. As the type needs them, `precision`, `scale`,
  `size`, `characterSet`, `withLocalTimeZone`, `fraction` and `srid` follow. On protocol 3 and 4,
  `TIMESTAMP` and its local time zone twin hold `size` 16 and `DATE` holds `size` 4. On protocol 5,
  they hold `precision` (0 to 9) and `DATE` holds no `size` ("select the table of types on
  protocol 5", "select a timestamp on protocol 4").
- A row. A NULL is `null`. An empty string is NULL: `''` and `CAST('' AS CHAR(3))` both came back as
  `null`, and a bound `""` was stored as NULL ("select an empty string", "lead: bind an empty string",
  "lead: the row of the empty string").
- Paging. The server returns the rows in the answer when the result has fewer than 1000 rows, and
  a handle when it has 1000 rows or more. A result of 999 rows came whole, and 1000 rows came as a
  handle with `numRowsInMessage` 0 ("select 999 rows", "select 1000 rows", "select 3000 rows").
  The documents say the same.
- `fetch` takes `resultSetHandle`, `startPosition` (a row, from 0) and `numBytes`. The answer
  holds `numRows` and `data`. `numBytes` is a soft limit. A fetch of 8000 bytes gave 477 rows, of 100 bytes 6 rows, and of 1 byte 1 row ("fetch the first page of 8000 bytes",
  "fetch a page in the middle with 100 bytes", "fetch one byte"). A fetch of 67108865 bytes, one more than the
  size that the documents give as the largest, was not refused and returned all 3000 rows ("fetch
  over the largest size"). A fetch of 0 bytes was refused with `Requested byte size has to be larger than 0`
  and the code `42X96` ("fetch zero bytes", in a session of its own). In the first run of the script, the
  same fetch in the main session got no answer for 60 seconds and ended the session. Not measured: why.
  A fetch at any position is allowed, in any order
  ("fetch the second page", "fetch the last rows"). A fetch at the position `numRows`, after it, or
  at -1 is `Invalid result set seek` with `42000` ("fetch at the end", "fetch after the end",
  "fetch at a negative position").
- The handle. `closeResultSet` takes `resultSetHandles`, a list, and closes all of them
  ("close two result sets in one command"). A second close is the error
  `Invalid ResultSet handle` with `34000`, and a fetch after the close is `Resultset handle 33 is
  unknown` with `42000` ("close the result set twice", "fetch after the close"). Handles count up in
  a session, from 1. Not measured: the number of open handles that a session allows, and what the server
  does with a handle that nobody closes.
- The size of the first message. Measured on 2026-10-11 with the probe, and not recorded because
  the file would hold 66 MB: `SELECT LPAD(1, 2000000, 1) FROM DUAL CONNECT BY LEVEL <= 40` gave a
  handle, `numRows` 40 and `numRowsInMessage` 33, so the first message held as many rows as fit in
  `maxDataMessageSize`, 67108864 bytes. Five such rows (10 MB) came whole. The root client reads a
  message of up to 64 MiB by default, which is the same size.
- The size of a request. Measured on 2026-10-11 with the probe, and not recorded: a statement of
  70 MiB answered `maximum statement length exceeded (67108864)` with `42000`, and the session
  stayed usable. A statement of 1900000 characters ran.
- `resultSetMaxRows` is an attribute, also on one request. A limit of 10 and a limit of 3 on the
  `execute` cut the result to that many rows, with `numRows` equal to the limit ("select with the
  row limit", "a row limit on the execute").
- Compression. Recorded in "send the credentials and ask for compression": with `useCompression` true in the second
  login message and `compressionEnabled` true in its attributes, the client sends each later message
  as a binary frame of zlib data, and the server answers with a binary frame of zlib data ("select with
  compression", "select the rows with compression", "fetch with compression"). A text frame sent
  after that was answered with a compressed binary frame as well ("a text frame after compression
  was turned on"). The login messages are not compressed. This is not the extension
  `permessage-deflate` of the WebSocket. The text of the answer to "select the rows with compression"
  is 2906 bytes, and the file keeps the text, so the size on the wire is not recorded.
- A redirect. The upgrade answers 101 (`Switching Protocols`) and nothing else was seen. Not
  measured: a redirect, and a limit on the rate of requests.
- Heartbeats. The documents say that the attribute `feedbackInterval` makes the server send Pong
  frames while a query runs. Not measured. The root client drops a pong.

## Types

The types are recorded in "select every row of types", "select the table of types on protocol 5",
"select decimals by precision" and.

- BOOLEAN is `true` or `false`.
- CHAR(n) is a string, padded with spaces to `n` (`"ab   "`). `n` is 1 to 2000 and the character
  set is `UTF8` or `ASCII`.
- VARCHAR(n) is a string. `n` is up to 2000000 (`maxVarcharLength`), and the character set is
  `UTF8` or `ASCII`. The text `héllo ☃ "quoted"` with a tab and a newline came back whole.
- DECIMAL(p,s) is a JSON number when `s` is 0 and `p` is 18 or less, and a JSON string in every other
  case. So DECIMAL(18,0), DECIMAL(9,0) and DECIMAL(1,0) are numbers, and DECIMAL(19,0), DECIMAL(10,2),
  DECIMAL(18,5), DECIMAL(18,18) and DECIMAL(36,0) are strings. The number 9007199254740993 arrived
  exact, so a client must read a number as text and never as a `float64`. A string holds the exact
  digits, such as `"1234567890123456789012345.6789012345"` and `"0.123456789012345678"`. The largest
  precision is 36 and the largest scale is 36 (`CAST(1.123 AS DECIMAL(36,36))` is an error, because
  the value is out of range, and the type is allowed). The smallest and largest values of
  DECIMAL(36,0) (36 nines) came back exact. A zero with a scale is `"0"`.
- The aliases `TINYINT`, `SMALLINT`, `INT`, `INTEGER` and `BIGINT` arrive as DECIMAL(3,0), (9,0),
  (18,0), (18,0) and (36,0), and `NUMERIC(5,2)` and `DEC(4,1)` as DECIMAL. `REAL`, `FLOAT` and `DOUBLE
  PRECISION` arrive as DOUBLE, and `BOOL` as BOOLEAN. `LONG VARCHAR` is VARCHAR(2000000), and
  `NCHAR(2)`, `NVARCHAR(3)` and `CHAR VARYING(3)` are CHAR and VARCHAR of UTF8 ("lead: integer
  aliases"). The server has no integer type on the wire.
- DOUBLE is a JSON number such as `1.5e+300` and `1e-07`. The largest value that the server stored
  was 1.7e308, and 1.797693e308 was refused as out of range. `5e-324` came back. A negative zero
  came back as `0.0`. `NaN` and `Infinity` are refused (`invalid character value for cast`, "select NaN",
  "lead: Infinity as a double"), and `1e308 * 10` is an error ("select a double overflow").
- DATE is a string `2026-10-11`. The smallest value `0001-01-01` and the largest `9999-12-31` came back.
- TIMESTAMP(p) is a string such as `2026-10-11 12:34:56.123000`, with 6 digits of fraction, for any
  precision `p` from 0 to 9. The default precision is 3. A value of TIMESTAMP(9) with 9 digits came back
  with 6 digits, so the last 3 digits are lost ("select precisions on protocol 5"). The text holds no
  zone. The smallest value was `0001-01-01 00:00:00.000000` and the largest `9999-12-31 23:59:59.999000`.
- TIMESTAMP(p) WITH LOCAL TIME ZONE has the same text and the same precision. The text is in the
  time zone of the session, which `getAttributes` and the login answer name as `timezone` (`CET`, and
  `ASIA/TOKYO` after `ALTER SESSION SET TIME_ZONE`). The value 12:34:56 stored in CET came back as
  19:34:56 in Tokyo, while a plain TIMESTAMP stayed 12:34:56 ("local time zone in Tokyo"). Measured on
  2026-10-11 in an earlier run of the script, not kept: the value `0001-01-01 00:00:00` was refused for
  this type with `22036` in CET, because the conversion between zones gives an invalid timestamp. The
  recording uses `0001-01-02` as the smallest ("insert the smallest values").
- INTERVAL YEAR(p) TO MONTH is a string such as `+000000005-03`. `p` is up to 9, the largest was
  `+999999999-11` and the smallest `-999999999-11`. A positive value starts with `+` and a negative
  value starts with `-`. The value 5 years and 3 months is `+000000005-03` in a column of `YEAR(9)`, and
  the number of digits of the year follows `p`.
- INTERVAL DAY(p) TO SECOND(f) is a string such as `+000000002 03:04:05.678000000`. The largest was
  `+999999999 23:59:59.999000000` and the smallest `-999999999 23:59:59.999000000`. The insert of nine
  digits of fraction (`.999999999`) kept only three, `.999000000`, for a column of `SECOND(9)`. Not
  measured: why, and whether another input keeps nine. The type holds `precision` for the days and `fraction` for the
  seconds on protocol 5 (`precision` 2 and `fraction` 3 for `INTERVAL '2 03:04:05.678' DAY TO
  SECOND`).
- GEOMETRY(srid) is a string in WKT, such as `POINT (1 2)`, `POLYGON ((0 0, 1 0, 1 1, 0 0))` and
  `LINESTRING (0 0, 1 1)`. The text is normalized (`POINT(1 2)` came back as `POINT (1 2)`). The
  column holds `size` 2000000 and `srid`, 0 or the one that was declared (4326).
- HASHTYPE(n BYTE) is a string of lower case hex digits with no dashes. A GUID such as
  `550e8400-e29b-41d4-a716-446655440000` of HASHTYPE(16 BYTE) came back as
  `550e8400e29b41d4a716446655440000` (`size` 32), and HASHTYPE(32 BYTE) as 64 digits.
- NULL. Each type gave `null` for a row of NULL ("insert a row of NULL").
- The type table is in step 8a below.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| DECIMAL INTEGER | integer | `int64` | `int64` | `DECIMAL` | yes |
| DECIMAL | decimal | `*apd.Decimal` | `*apd.Decimal` | `DECIMAL` | yes |
| DOUBLE | float | `float64` | `float64` | `DOUBLE` | yes |
| CHAR | string | `string` | `string` | `CHAR` | yes |
| VARCHAR | string | `string` | `string` | `VARCHAR` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIMESTAMP | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP` | yes |
| TIMESTAMP WITH LOCAL TIME ZONE | timestamp | `time.Time` | `time.Time` | `TIMESTAMP WITH LOCAL TIME ZONE` | yes |
| INTERVAL YEAR TO MONTH | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL YEAR TO MONTH` | yes |
| INTERVAL DAY TO SECOND | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL DAY TO SECOND` | yes |
| GEOMETRY | geometry | `string` | `string` | `GEOMETRY` | yes |
| HASHTYPE | binary | `[]byte` | `[]uint8` | `HASHTYPE` | yes |
<!-- /dbimp:types -->

This table is the mapping of step 8a. Ken reviewed it in step 9 item 4 (D206), and `TestTables` makes the same
table from the code. The notes:

- `DECIMAL INTEGER` is a column of DECIMAL with scale 0 and precision 18 or less, and `DECIMAL` is every
  other column. The Go type depends on the column and not on the value, as D183 decides for Snowflake. A
  column of `BIGINT` is DECIMAL(36,0), so it is `*apd.Decimal`.
- A decimal and a large number arrive as a number or as a string by the rule above, so the driver reads
  each as text and gives the type of its column.
- The Go type of `GEOMETRY` is the text of the server, in WKT. The kind geometry leaves the form to a
  decision of the driver.
- `INTERVAL YEAR TO MONTH` of the precision 9 holds up to 11999999999 months, which is more than the
  `Months int32` of `dbimp.Interval`. `INTERVAL DAY TO SECOND` of the precision 9 holds up to 999999999
  days, which fits `Days int32`.
- `HASHTYPE` of 16 bytes has the form of a UUID. The mapping keeps `[]byte` for every size.
- `TIMESTAMP WITH LOCAL TIME ZONE` has no offset in its text. The driver needs the zone of the session.
- A `CHAR` keeps the padding that the server sends.

### Doubtful types

Ken decided each one in D206: a value of `INTERVAL YEAR TO MONTH` that overflows is an error, `HASHTYPE` is a
`[]byte` for every size, `GEOMETRY` is its text, `CHAR` keeps its padding, and an empty string is NULL (D207).

- `INTERVAL YEAR TO MONTH`: the overflow of `Months int32` for a year precision above 178 million
  (both models named it).
- `HASHTYPE(16 BYTE)`: `uuid.UUID` is the alternative for a type of 16 bytes (both models named it).
- `TIMESTAMP WITH LOCAL TIME ZONE`: the zone name of the session, such as `ASIA/TOKYO`, is in upper
  case, and a time in the hour of a change to winter time has two meanings. Not measured: how the server
  decides, and whether `timeZoneBehavior` (`INVALID SHIFT AMBIGUOUS ST`) says it.
- `CHAR`: the padding. One model proposed to trim it.
- `GEOMETRY`: the text loses nothing, and the form of the Go type is the choice.
- `TIMESTAMP(p)` above 6: the text holds 6 digits, so the last digits are lost on the wire. A value of `TIMESTAMP(9)`
  that the driver bound with 9 digits was stored with 6 (measured with the driver on 2026-10-11, not recorded).
- `DOUBLE`: a value of 17 significant digits can come back with one unit less in its last digit. The value
  `123456789.12345679` was bound and came back as `123456789.1234568`, and a literal made with `CAST` gave the same.
  The values `1.0000000000000002`, `0.30000000000000004`, `3.141592653589793` and `0.1` came back whole (measured with the
  driver on 2026-10-11, not recorded). The text of the answer has up to 17 digits (recorded: "select every row of
  types", where the row 10 holds `123456789.12345679`), so the loss is in what the server stores.
- `VARCHAR`: an empty string is NULL, so a string column cannot hold an empty string.

## Parameters

- The driver can use a prepared statement. `createPreparedStatement` takes `sqlText`, and it answers
  with `statementHandle` and `parameterData.columns`, one column for each `?` with its type ("prepare an
  insert with every kind of column"). `executePreparedStatement` takes `statementHandle`, `columns`,
  `numColumns`, `numRows` and `data`, where `data` is a list of columns as in a result set, so it binds
  many rows at once. `closePreparedStatement` takes `statementHandle` and answers `{"status":"ok"}`.
  Handles count up in a session ("execute the prepared insert with strings and numbers").
- The parameters are positional `?`. A named parameter is refused: `Feature not supported: host
  parameter specification` with `0A000` ("prepare with a named parameter"). `execute` with a `?`
  is refused: `query has external parameters and can only be used as prepared statement` with `04000`
  ("a placeholder in an execute"). A `?` in a string is not a parameter ("a question mark in a string").
- The type of a parameter comes from the statement. `INSERT ... VALUES (?, ...)` gave the types of the
  columns, and `SELECT ?, ?` gave `VARCHAR(2000000)` for each ("prepare a select with parameters in the
  select list"). `SELECT ... WHERE ID = ? OR V = ?` gave the types of the columns ("prepare a select with a typed
  parameter").
- The JSON form of a value. The server read the value by the type of the declared column:
  - A boolean needs `true` or `false`. The string `"true"` is refused: `getBool: JSON value is not a
    bool.` ("execute the prepared insert with a string for a boolean").
  - A VARCHAR needs a string. The number 12345 is refused: `getString: JSON value is not a string.`
    ("execute the prepared insert with a number for a string").
  - A DECIMAL took a number (123456789012345678 for DECIMAL(18,0), 1.5 for DECIMAL(10,2)) and a string
    (`"12345.67"`, `"123456789012345678901234567890123456"`) ("lead: a number for a decimal with a
    scale", "lead: a string for a decimal with a scale"). A value with too many digits was rounded to
    the scale: `"1.555"` for DECIMAL(10,2) was stored as 1.55 ("lead: a number with too many digits").
  - A DOUBLE needs a JSON number with a fraction or an exponent. The integer `1` is refused:
    `getDouble: JSON value is not a double.`, and `1.0` and `1.5e300` are taken ("lead: an integer for a
    double", "lead: a double with an exponent"). The string `"NaN"` is refused too ("lead: a string for a
    double"). `exasol-driver-go` has a workaround for this (its issue 108).
  - A DATE, a TIMESTAMP, a local time zone TIMESTAMP, an interval, a GEOMETRY and a HASHTYPE took a
    string in the text form that the server writes, such as `"2026-10-11"`, `"2026-10-11 01:02:03.456789"`,
    `"+000000012-03"`, `"+000000002 03:04:05.678000000"`, `"POINT (3 4)"` and
    `"550e8400e29b41d4a716446655440001"`. A bad date is the error `invalid value for YYYY format token`
    with a code in the text and `00000` in the field ("execute the prepared insert with a bad date").
    The server also took `VARCHAR` declared for a DATE column in the `columns` of the request, and
    converted the text ("lead: declare a wrong column type").
  - A NULL is `null`. A bound empty string is NULL.
- Several rows. `numRows` of 3 inserted three rows ("execute the prepared insert with three rows").
  A `SELECT` with more than one row of parameters is refused: `Feature not supported: Prepared statement
  with multiple result tables` with `0A000` ("execute the typed select with two rows"). A DML with
  three rows of one parameter changed 3 rows ("execute the DML").
- A frame that gives fewer columns than the statement has is refused: `Too few parameters for statement.`
  with `XQ014` ("execute the prepared insert with a missing column").
- A closed or an unknown handle is an error: `Statement handle not found.` with `42X01` for an execute
  and `Statement handle is unknown` with `42000` for a close ("execute a closed prepared statement",
  "close the prepared insert again", "close an unknown prepared statement").
- A statement with no parameter takes `numColumns` 0 and `data` `[]`, and answers as `execute` does
  ("prepare with no parameter", "execute with no parameter"). `createPreparedStatement` of a statement
  also holds `results`, with a result set of no rows for a `SELECT` and a `rowCount` of 0 for the others.
- A syntax error in the statement is an error of `createPreparedStatement` ("prepare a statement with a
  syntax error"). DDL can be prepared ("prepare DDL").
- If the driver binds no parameters itself, the parser for placeholders of the root package (D34) can
  turn `?` into literals. The server binds them, so a driver does not need to.

## Transactions

- The attribute `autocommit` is true at the login. `setAttributes` with `{"autocommit":false}` answers
  `{"status":"ok"}` and starts a transaction at the next statement. `getAttributes` reports
  `autocommit` and `openTransaction`: 0 before the insert and 1 after it ("turn autocommit off",
  "attributes with an open transaction").
- `ROLLBACK` and `COMMIT` are statements of `execute`, and they answer with a `rowCount` of 0. The row
  that was inserted before the `ROLLBACK` was gone, and the row before the `COMMIT` stayed ("rollback",
  "the row is gone after the rollback", "commit", "the row stays after the commit").
- A `CREATE TABLE` in a transaction was undone by `ROLLBACK`: the table was not in `EXA_ALL_TABLES`
  afterwards ("DDL in a transaction", "rollback after DDL", "the table after the rollback"). So DDL does
  not commit by itself with `autocommit` off.
- A failed statement does not end the transaction. A primary key violation left the earlier rows in place
  ("a failed statement in a transaction", "the transaction after the failure").
- With `autocommit` on, a `ROLLBACK` has no effect on a row that was committed ("insert with autocommit",
  "rollback with autocommit", "the row stays").
- `BEGIN` and `START TRANSACTION` are syntax errors, `SAVEPOINT` is `Feature not supported: savepoint
  statement`, and `SET TRANSACTION ISOLATION LEVEL SERIALIZABLE` is `Feature not supported: isolation
  level SERIALIZABLE`, all with `0A000` or `42000` ("begin is not a statement", "savepoint", "set a
  transaction isolation level"). `COMMIT` with nothing open is accepted ("commit with nothing open").
- The attribute `snapshotTransactionsEnabled` is true. Not measured: what the isolation is, and how two
  sessions see each other.
- `exasol-driver-go` turns `Begin` into an error when `autocommit` is on (`begin not working when
  autocommit is enabled`). A new driver can set `autocommit` to false for the transaction and back after
  it. That is a choice of step 9 item 7.

## Errors

- The shape. `{"status":"error","exception":{"text":...,"sqlCode":...}}`. The code is `sqlCode`, a string
  of five characters, and the text ends with `(Session: <id>)`, except for a login error, which has no
  session. An answer can hold `attributes` as well. A frame that the server cannot parse has the code
  `00000`.
- Every error is in a frame. The upgrade answers 101 and has no error status in the recordings.
- The codes that were recorded:

  | Code | Cause (recorded name) |
  | --- | --- |
  | `00000` | A frame with no `command`, a member of the wrong type, an unknown command, a read only attribute, a value that the parameter binder cannot read ("a frame that is not JSON", "execute with a number for sqlText", "set a read only attribute") |
  | `04000` | A virtual schema with no adapter, a `?` in an `execute` ("lead: a virtual schema", "a placeholder in an execute") |
  | `08004` | A wrong password, a wrong user, a wrong token ("login with a wrong password: send the credentials", "login with a wrong user: send the credentials") |
  | `0A000` | A feature that the server does not offer: a unique constraint, a check constraint, a savepoint, a local import ("unique constraint is not offered", "lead: a check constraint") |
  | `22001` | A string that is too long ("string too long") |
  | `22003` | A number out of range ("number out of range", "select a double overflow") |
  | `22012` | A division by zero ("division by zero") |
  | `22018` | A bad cast, a bad date ("bad cast", "bad date") |
  | `22036` | A bad time zone conversion ("insert the smallest values") |
  | `27001`, `27002`, `27003` | A violation of NOT NULL, of the primary key, of a foreign key ("not null violation", "primary key violation", "foreign key violation") |
  | `34000` | A result set handle that is closed or unknown ("close the result set twice") |
  | `40001` | A negative `queryTimeout` ("a negative query timeout") |
  | `42000` | A syntax error, an object that is not found, a statement handle or result set that is unknown, an empty statement ("syntax error", "missing table", "fetch with an unknown handle", "execute with an empty statement") |
  | `42500` | A privilege that is missing, a name that exists, a user that does not exist ("select without the privilege", "table that exists", "a privilege error for the administrator") |
  | `42X01`, `42X80`, `42X96` | A statement handle that is not found, an empty batch, a `numBytes` of 0 |
  | `R0001` | A query that the timeout ended ("a query that runs over the timeout") |
  | `R0003` | A query that `abortQuery` ended ("abort a running query") |
  | `XQ014` | Too few parameters ("execute the prepared insert with a missing column") |

- A missing object. A table, a schema and a column that do not exist all give `42000` with `object X not
  found` and the line and the column (`[line 3, column 2]` for a name on the third line, "an error that
  carries a line"). `CREATE TABLE` of an existing name is `42500` `object DBIMP.DML already exists`. A table
  name that is a keyword, such as `ROWS` and `LOW`, is `table name not allowed since it is a keyword`.
- An error before any row. Every error that was recorded came with no rows. An error in the 3000th row of a
  large result (`1/(2999 - ID)`) was the error of the `execute`, with no handle ("error in a late row of a
  large result"). So the server computes a whole result before it answers, and no error arrives after some
  rows. Not measured: an error during a `fetch` of a result that was computed.
- A batch with an error. `executeBatch` runs the statements in order. When one fails, the answer has
  `"status":"error"`, an `exception`, and `responseData.results` with the results of the statements before the
  failure, and the statements before it stay done ("a batch with an error in the middle", "count the rows after
  the failed batch").
- The errors that mean the request did not reach the server: a failed dial, a failed upgrade, and a write
  that failed before the frame was sent. The root client returns `driver.ErrBadConn` for a dial. After a frame
  was written, an error is never a bad connection, because the statement can have run (D8).
- An error of the login is `08004` and an authentication error for `dbimp.ErrAuthentication` (D197). The
  text is `Connection exception - authentication failed.`. The server does not say whether the user or the
  password was wrong ("login with a wrong password: send the credentials", "login with a wrong user: send the credentials").
- A session that the server ended has no error frame. The frame after the idle time got no reply and the
  connection ended with end of file ("lead: a statement after the idle time"). The recorder keeps that as the
  close code 1006.

## Cancellation and timeouts

- The query timeout. `{"command":"setAttributes","attributes":{"queryTimeout":1}}` makes the server end a query
  that runs longer than that many seconds, with `Query terminated because timeout has been reached.` and
  `R0001`. The session worked after it ("a query that runs over the timeout", "the session after the
  timeout"). The valid range is 0 and 1 to 999999 seconds, and -1 is `40001` ("a negative query timeout"). A string is
  `getInt: JSON value is not an integer.` ("a query timeout of a wrong type"). `ALTER SESSION SET QUERY_TIMEOUT = 5` sets the
  same attribute ("lead: a session parameter").
- `abortQuery` has no answer of its own. Sent one and a half seconds after a long `execute`, it made the
  `execute` answer `Client requested execution abort.` with `R0003`. The session worked after it ("abort a
  running query", "the session after the abort"). Sent when nothing runs, it had no effect, produced no answer, and the
  session worked ("abort when nothing runs", "the session after the idle abort"). So a driver must read the answer of
  the aborted query and never wait for an answer of the abort. The recorder keeps the abort as an exchange with no
  reply, before the exchange that it ended.
- A client that goes away. A session that sent a long query and then closed its socket was in
  `EXA_DBA_SESSIONS` half a second later, with its statement, and was gone five seconds later ("start a long query and
  leave", "the sessions right after the client left", "the sessions five seconds later"). So the server stops the
  query of a client that disconnects, within five seconds. The client closed with a close frame and waited for the
  reply.
- A context. The old driver sends `abortQuery` when its context ends, and then returns, with the reader still
  waiting for the answer. The next command can then read that answer. A new driver must read the answer of the
  aborted query before it sends the next frame (see "Faults").
- The idle timeout. `ALTER SESSION SET IDLE_TIMEOUT = 1` is accepted, and `getAttributes` does not report it. The
  session was closed by the server after one second with no close frame: a statement three seconds later got end of file
  ("lead: an idle timeout statement", "lead: a statement after the idle time"). The default value is 86400 seconds, which is a
  day (measured with the driver on 2026-10-11 in `EXA_PARAMETERS`: `IDLE_TIMEOUT` has 86400 for the session and for the
  system, and `QUERY_TIMEOUT` has 0).
  The attribute `feedbackInterval` and a client ping can keep a session alive. Not measured.
- A frame that cancels a statement follows the compression of the session. With compression on, the server refuses a
  text frame while a statement runs: it answered the `abortQuery` that the driver sent as text with the error
  `WebSocket: readMessage: Text data frame was received, but compression is enabled.` and the code `00000`, and
  the next frames got `Received packet type not expected while command execution.` with `42X99`. The driver sends
  `abortQuery` as a binary frame of zlib data when compression is on, and the abort then worked (measured with the driver
  on 2026-10-11, not recorded). The recordings of the abort are of a session with compression off.
- A context that ends in the instant after the driver wrote a frame used to close the connection. The guard of `Write` in
  the root client closed the socket when the context ended before its call returned, even when the frame was whole on the
  wire, and the same held for `Read`. The driver then lost the session and returned the error of the context. A unit test
  that cancelled the context when the scripted server read the request met this in about one run of five under heavy load
  (measured on 2026-10-11). The cause was that the body of an upgraded response has no write deadline, so the guard
  always fell back to a close. D217 fixed it: the client keeps the TCP connection of the upgrade, a context that ends
  during a call sets a deadline in the past on it, and a context that ends after the call did its work changes nothing.
  The driver reads with `context.WithoutCancel`, so a read is never interrupted by the context of the caller.
- A write to a table waits while a transaction of another session holds the same table. A test that inserted a row in
  a transaction and then inserted a second row in the same table from another session did not return until the
  transaction ended (measured with the driver on 2026-10-11, not recorded).
- A fetch of 0 bytes is answered with an error and not with silence ("fetch zero bytes"). An early probe of
  step 2 took a long time for it, because the probe read the answer late. The recording shows the answer.

## Statements

- One statement for each `execute`. Two statements with a `;` between them are a syntax error:
  `syntax error, unexpected SELECT_, expecting END_OF_INPUT_` ("two statements with a semicolon"). A single trailing `;`
  is accepted ("select a statement with a trailing semicolon"). A driver that splits a script must send each
  statement alone, or send the list with `executeBatch`.
- `executeBatch` takes `sqlTexts`, a list, and answers with one result for each statement ("two statements in one
  batch"). An empty list is the error `Statement missing for batch execution.` with `42X80` ("an empty batch"). A
  batch can hold a statement with a large result, which then has a handle ("a batch of one large result").
- A comment. `--` to the end of the line and `/* */` are accepted before, inside and after a statement
  ("a block comment"). A `;`
  or a `?` inside a string is not a separator and not a parameter ("a string with a semicolon", "a question mark in a
  string"). A statement of only a comment answers with a `rowCount` of 0, and an empty string is an
  error: `empty sql statement not allowed` with `42000` ("execute with a comment only", "execute with an empty
  statement").
- A result is a `resultSet` or a `rowCount`. `rowCount` holds the number of rows that an `INSERT`, `UPDATE`,
  `DELETE` and `MERGE` changed, and 0 for DDL, `TRUNCATE`, `COMMIT` and `ROLLBACK` ("update two rows" gave 2, "update no
  row" gave 0, "insert three rows" gave 3). There is no last insert id. An `IDENTITY` column makes the value on the server
  ("lead: identity and default").
- `LIMIT 3 OFFSET 5` and `LIMIT 5, 3` both work ("lead: limit and offset", "lead: limit with a comma").
- The statements of CRUD, the schema and the features of the survey are in `testdata/exasol/features.json`. The names in
  quotes in this document are the names of the requests.
- The statements that the server refuses over the WebSocket: `IMPORT` and `EXPORT` of a local file. The answer is
  `Feature not supported: IMPORT and EXPORT of local files is only supported via JDBC (except prepared statements) or
  EXAplus` with `0A000` ("lead: export to a local file", "lead: import from a local file"). `exasol-driver-go` imports a
  local file by a proxy connection that it opens itself, and it rewrites the statement. That is not measured here.
- A name that is a keyword cannot be a table name. `ROWS`, `LOW` and `PAD` are some. The statement
  `SELECT 1 AS PAD` is a syntax error, and `getKeywords` returned 876 keywords (measured with the probe on 2026-10-11, not recorded).
- `UPSERT INTO` is a syntax error, and `MERGE` is the form that the server has. `CREATE INDEX` is a syntax error.
  `CREATE TABLE` takes `DISTRIBUTE BY` and `PARTITION BY` ("lead: upsert as a keyword", "lead: create an index", "lead:
  distribute and partition").

## Principals

- `dbrun` makes one user, `sys`, which is the administrator. There is no ordinary user, and the manifest says so in
  `noOrdinaryUser` (as the manifest of Snowflake does).
- Item 8 of step 6 recorded a user that the script made, `DBIMP_LOW`, with `CREATE SESSION` and nothing else, as the
  principal that an ordinary user would be.
- It logged in ("login as the user with one privilege: send the credentials"). It could read the version from `EXA_METADATA`
  (`2026.2.0-nano.6`) and its own name from `CURRENT_USER` (`DBIMP_LOW`) ("read the version without the privilege",
  "read the own user"). The current schema of the new user is NULL.
- It could not read a table of another schema: `insufficient privileges: SELECT on table DML` with `42500` ("select
  without the privilege"). It could not create a table: `insufficient privileges for creating table` ("create a table
  without the privilege"). It could not read `EXA_DBA_SESSIONS`: `insufficient privileges for accessing view
  EXA_DBA_SESSIONS` ("read the system table of sessions without the privilege").
- `sys` read `EXA_DBA_SESSIONS`, `EXA_ALL_USERS` and `EXA_ALL_TABLES`. `EXA_ALL_USERS` showed only `SYS` at the start of
  the run ("the users that exist").
- The version. The login answer holds `releaseVersion` (`2026.2.0-nano.6`), which every user gets. The statement
  `SELECT PARAM_VALUE FROM EXA_METADATA WHERE PARAM_NAME = 'databaseProductVersion'` gives the same text to a user with
  one privilege ("version from the metadata"). The driver file of `usql` sets no statement for the version
  (`usql/drivers/exasol/exasol.go`), so step 16 needs to name one.
- The integration tests make an ordinary user with `CREATE USER`, `GRANT CREATE SESSION` and `GRANT SELECT ON SCHEMA`
  for the schema of the test. That user reads the tables, the views and the system tables `EXA_ALL_*` for the objects of
  that schema. It gets `42500` for each write and for `EXECUTE SCRIPT` and a call of a script function, which need the
  privilege EXECUTE (measured with the driver on 2026-10-11, not recorded).
- The user name is case-insensitive: `sys` and `SYS` both logged in ("send the credentials of a lower case user").

## Flavors

None. The Exasol WebSocket API has one product behind it. The protocol versions 3, 4 and 5 are versions of one API
and not flavors, and "Responses" lists their differences. Other products do not speak it. The Community Edition 2025.2.1 of
the `dbmeta` entry is an older release of the same product and was not run here.

## Interfaces

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | Connect opens a WebSocket and logs in, and the connector holds nothing else. |
| `io.Closer on the connector` | no | The connector holds no transport or connection, because each connection owns its WebSocket. |
| `driver.Pinger` | yes | Ping sends getAttributes, which costs the server nothing. |
| `driver.SessionResetter` | no | A transaction is the only state that database/sql does not end, and it ends it before it reuses the connection (D102). |
| `driver.Validator` | yes | IsValid is false after the server ended the session or the driver closed it after a fault, so database/sql drops the connection. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64, an *apd.Decimal and the civil types of the root package, which the driver writes in the form that the server reads (D208). |
| `driver.QueryerContext` | yes | A statement with no argument goes in execute, and one with arguments is prepared, executed and closed (D208). |
| `driver.ExecerContext` | yes | Exec reads the rowCount of the result. It has no last insert id. |
| `driver.ConnPrepareContext` | yes | PrepareContext sends createPreparedStatement, so a syntax error shows at once, and the server binds the arguments (D208). |
| `driver.ConnBeginTx` | yes | BeginTx turns autocommit off, and Commit and Rollback turn it on. ReadOnly and an isolation level fail (D209). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A statement is one for each request, so an answer holds one result (recorded: "two statements with a semicolon"). |
| `driver.RowsColumnTypeScanType` | yes | The type of each column gives its Go type (D206). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of each column names its database type, such as DECIMAL or INTERVAL DAY TO SECOND. |
| `driver.RowsColumnTypeLength` | yes | A CHAR and a VARCHAR have their size in characters, and a HASHTYPE its size in bytes. |
| `driver.RowsColumnTypeNullable` | yes | The answer does not name NOT NULL, so every column can be NULL. |
| `driver.RowsColumnTypePrecisionScale` | yes | A DECIMAL has its precision and its scale. |
<!-- /dbimp:interfaces -->

## Faults

The faults of `exasol-driver-go` v1.1.1 (the driver that `usql` uses now), which a new driver must not repeat. Each was
found in its source, which was read on 2026-10-11, and not run. The `usql` driver file adds no code beyond an error
formatter (`usql/drivers/exasol/exasol.go`).

- A DECIMAL number is read into a `float64`, so a value above 2^53 loses digits. The driver has a workaround for the case
  of scale 0 (its issue 113), and it still reads a DECIMAL with a scale from a string.
- `Send` runs the read in a goroutine. When the context ends, it sends `abortQuery` and returns, and the goroutine still
  waits for the answer. The next command can read the answer of the aborted query, because nothing reads it. Release 1.1.1
  serialized the access to the socket, but the answer of the aborted query is still not read.
- `Close` of the result and `Close` of the connection use `context.Background()` for their commands, so a hung server hangs
  them. The connection also holds the context of `Connect` in a field (`Connection.Ctx`).
- `Begin` returns an error when `autocommit` is on. It takes no `TxOptions`.
- Every value of a prepared statement goes through `convertArg`, which handles a double, a date and a timestamp and leaves
  the rest to `encoding/json`. So a `[]byte` goes as a Base64 string, and a decimal or an interval has no conversion.
- `ColumnTypeNullable` says that every column can be NULL, and `ColumnTypeScanType` returns `sql.RawBytes` for the types
  that arrive as strings (`DATE` and `TIMESTAMP` are left as `interface{}`), so a caller gets a string for a date.
- `Query` of a statement with arguments prepares it, executes it and closes the statement, which is three round trips for one
  query.
- The driver writes to `stderr` through a global logger that the caller can set (`logger.SetLogger`), which breaks hard rule 2
  of this repository.
- `localimportencryption` is deprecated, and the driver falls back to plain text for the proxy of a local import on Exasol 8.
- `fetchsize` defaults differ between the README (128 x 1024 kB) and the builder (2000 KiB).
- The driver imports `gorilla/websocket`, `golang.org/x/sync` (its `errgroup` runs every `Exec`) and the error reporting
  package of Exasol. This repository allows none of them (hard rule 6).

## Second opinions

Step 5a. Gemini 3.1 Pro (`gemini-3.1-pro-preview`) and DeepSeek V4 Flash (`deepseek-v4-flash`) answered the four questions on
2026-10-11. Qwen 3.8 Max timed out twice, Kimi K3 twice, DeepSeek V4 Pro once (it ran out of tokens in its thinking) and once
more (a timeout), and Qwen 3.7 Plus once, so those answers are not recorded. Every answer is a lead. The survey in
`testdata/exasol/features.json` holds each entry with its verdict from the server. Where a model was wrong:

- Gemini said that a primary key and a foreign key are enforced by default and can be `DISABLE`d. The recording used
  `ENABLE` and the server enforced both ("primary key violation", "foreign key violation"). The default is not measured.
- DeepSeek said that `TINYINT`, `SMALLINT`, `INT` and `BIGINT` are native types with their own ranges. The server names
  them DECIMAL on the wire ("lead: the columns of the aliases").
- DeepSeek said that a foreign key is metadata only and that a unique constraint is supported. The server enforced the
  foreign key, and it refused the unique constraint (`0A000`).
- Gemini named the parallel sub-connections as a way to import and export. The server has the commands
  `requestParallelConnections` for protocol 5 and `enterParallel` for protocol 3, and both answered `numOpenConnections` 1 ("lead: request parallel
  connections on protocol 5", "lead: enter parallel on protocol 3"). Not measured: the use of the sub-connection.
- Gemini said that `UPSERT` is not a keyword. That was right ("lead: upsert as a keyword").
- Gemini said that an index is not supported. That was right, and `CREATE INDEX` is a syntax error.

Step 7. The same two models were asked what step 6 missed. Each lead was sent to the server:

- Gemini: attributes with `setAttributes` (done, and `dateFormat` and `timestampFormat` do not exist as writable ones: `Cannot set
  read-only protocol attribute: dateFormat` and `Protocol attribute is undefined`), a keepalive frame (the documents give a WebSocket
  ping, not measured), an idle timeout (`ALTER SESSION SET IDLE_TIMEOUT` is accepted and ended an idle session), `DECIMAL(36,36)`
  (accepted as a type, a value of 1.123 is out of range), the implicit commit of DDL (the DDL was undone by `ROLLBACK`), `IDENTITY` and
  `DEFAULT` (work), `LIMIT` and `OFFSET` (work), `EXPORT ... INTO CSV` (refused over the WebSocket), a Lua script (works with a `DOUBLE`
  argument and `EXECUTE SCRIPT`, and a `DECIMAL(9,0)` argument gave `bad argument (decimal expected, got number)`), the integer
  aliases (DECIMAL on the wire), and a login with a token (a bad token is refused, a good one is not measured).
- Gemini said `execute` can take `maxRows`. The member does not exist. The attribute is `resultSetMaxRows` ("a row limit on the execute").
- DeepSeek: a `logout` command (the server does not know it: "lead: a logout command"), a `timestampFormat` attribute (it does not exist), a
  `ping` command (there is none, and the documents give the WebSocket ping), a per-request `attributes` member (works for
  `resultSetMaxRows`), `NaN` and `Infinity` as doubles (refused), `INSERT ... DEFAULT VALUES` (works), `CREATE SCRIPT` and `EXECUTE SCRIPT` (work),
  `EXPORT` (refused over the WebSocket), `LIMIT ... OFFSET` (works), a prepared `CAST(? AS DECIMAL(5,2))` with a string or a number (a
  string and a number both work for a decimal column), a login with a token (see above).
- Still not measured: a ping from the client, the keepalive that stops the idle timeout, the heartbeat, the use of a sub-connection
  after `requestParallelConnections`, the import and export through a proxy connection, and a good access token.

Step 8a. The two models reviewed the type table on 2026-10-11 (Gemini 3.1 Pro and DeepSeek V4 Flash). Each point is a lead, and Ken decides.

- Both named the overflow of `Months int32` for `INTERVAL YEAR(9) TO MONTH` (999999999 years is 11999999988 months). Gemini proposed
  `string` for that type, and DeepSeek proposed a wider `Months`. The measurement agrees with the overflow.
- Both named `HASHTYPE(16 BYTE)` as a possible `uuid.UUID`. The measurement shows the same 32 hex digits that a UUID has, with no
  dashes. The type also has 32 bytes (64 digits), which is no UUID, so `[]byte` for every size is the proposal, and `uuid.UUID` for
  the size of 16 bytes is the alternative.
- Both named the padding of `CHAR`. Gemini left the choice to the use. The mapping keeps the server's text.
- DeepSeek said that `DECIMAL` with scale 0 and precision 18 or less as `int64` is doubtful because one SQL type changes its Go type by
  column, and that the aliases should map to `int64`. The aliases arrive as DECIMAL(36,0) for `BIGINT`, so they cannot (the wire holds
  no alias). The rule by column is what D183 does for Snowflake.
- DeepSeek said that a JSON number must be read as text so that 9007199254740993 stays exact. The measurement agrees, and it is in
  "Types".
- DeepSeek said that the zone name `ASIA/TOKYO` needs normalizing to `Asia/Tokyo` for `time.LoadLocation`, and that a time in the
  hour of a change of the clock is ambiguous. Both are real. Not measured: how the server decides the ambiguity.
- DeepSeek said that `GEOMETRY` as WKT loses the SRID. The column holds the `srid` in its `dataType`, so the driver can keep it
  in the column type and not in the value.
- DeepSeek named that `TIMESTAMP(p)` above 6 loses digits, and that `VARCHAR` cannot hold an empty string. Both are measured and are
  facts of the server.

## Open questions

Ken answered the questions of step 9 on 2026-10-11 (D203 to D214). These questions are open:

- The default of the key `protocol`. Ken named the key and gave no default. The driver uses 3, the version of every
  recording, and the server answers the version that it was asked for when it is 3, 4 or 5 (D205). Protocol 5 gives the
  precision of a TIMESTAMP, and the document of the API says that it needs Exasol 8.32.0 or later.
- The wait for the answer of an aborted statement. The driver waits 10 seconds and then closes the session (D211).
  Ken gave no limit.
- Whether the driver supports the local import and export through a proxy connection, as `exasol-driver-go` does. Ken's
  answers name none, so the driver sends the statement as it is, and the server answers `0A000` (recorded: "lead: import
  from a local file"). A caller imports through `INSERT` and the prepared statement.
- The version for `usql` and `dbmeta` (step 16). `usql` sets no statement for the version, and the driver gives none (D181
  does not apply, because the server has the statement `SELECT PARAM_VALUE FROM EXA_METADATA WHERE PARAM_NAME =
  'databaseProductVersion'`, which a user with one privilege can run, recorded as "version from the metadata").
- A session that the server ended after its idle time of 86400 seconds gives an error on its first statement, and the
  second statement works, because `database/sql` drops the connection (`TestIntegrationFeatures`, the subtest
  `idle_timeout`). Ken decided that the driver only documents this. A caller that holds a connection for a long
  time sets `SetConnMaxIdleTime` on the pool to a value below the idle timeout of the server.
- D215, the change of rule 4 of AGENTS.md for the rows, the statement and the transaction, is accepted by Ken.
- The shared test of the contract. `RunContract` sends HTTP, so `dbimptest` gets a second set of cases for a WebSocket,
  which `Contract.WebSocket` selects ([DESIGN.md](DESIGN.md), "The scripted server and the contract"). Ken reviews it with
  the staged changes.
- `TIMESTAMP WITH LOCAL TIME ZONE` before the rules of a time zone began: the server and Go can hold different data for the
  zone name `CET` in the years before 1893, so the same stored value reads back as a different instant. The round trip test
  uses 1970-01-02 as its oldest value for this type, because CI failed on the year 1 with a Go that resolves `CET` to a fixed
  offset (2026-10-11).

## Integration tests

The integration tests of the driver read `EXASOL_DSN`, the DSN of the administrator, and skip when it is empty (hard rule 9).
`dbrun` starts the release `exasol-2026.2.0` with one user, `sys`, and prints its URL:

    (cd ../dbmeta/test && go run ./cmd/dbrun start exasol-2026.2.0)
    export EXASOL_DSN=$(cd ../dbmeta/test && go run ./cmd/dbrun dsn --json exasol-2026.2.0 | jq -r '.[0].url')
    go test -race -count=1 -run Integration -v ./exasol/...

The URL holds `validateservercertificate=0`, because the certificate of the container is for `localhost` and no authority signed it.
The workflow starts each release that `dbrun list` names for the product `exasol`, and sets `EXASOL_DSN` to that URL. It needs
no other change, because it reads the folders under `testdata/`.

The ordinary user. `dbrun` makes no ordinary user, so `EXASOL_ORDINARY_DSN` is empty in the workflow, and each test makes one
with `CREATE USER` (the name starts with `DBIMP_USER_`), `GRANT CREATE SESSION` and `GRANT SELECT ON SCHEMA` for the schema of
the test. A caller can set `EXASOL_ORDINARY_DSN` to the DSN of a user that it made. The administrator makes each schema, which
starts with `DBIMP_`, and each test drops its schema and its user at the end, even when the test fails. Each test runs as the
administrator and as the ordinary user. The ordinary user reads what the administrator wrote, and the server refuses each of its
writes with `42500`.

The tests, and what each one holds:

- `TestIntegrationVersion`: the version from `EXA_METADATA`, the ping, and the user.
- `TestIntegrationCRUD`: three tables, one of them with a foreign key to another, and each statement of CRUD, `MERGE`,
  `TRUNCATE`, `LIMIT` with `OFFSET` and with a comma, `INSERT ... DEFAULT VALUES`, and the three statements that the server
  refuses (`UPSERT`, `IMPORT` and `EXPORT` of a local file). Exasol has no index, so no table has one.
- `TestIntegrationSchema`: the schema and the option `WithSchema`, a table, `ALTER TABLE`, the primary key and its violation, the
  foreign key and its violation, a view, defaults, identity, a comment, distribution and partition, and the index, the unique
  constraint and the check constraint that the server refuses.
- `TestIntegrationFeatures`: one subtest for each feature of `features.json`: scripts, a function of a script, a virtual
  schema, `CONNECT BY`, `PREFERRING`, `QUALIFY`, the local time zone, parallel connections, `executeBatch`, an abort by the
  context, the query timeout, the row limit, compression, transactions, a savepoint and an isolation level, which the server
  refuses, `loginToken`, the idle timeout, the metadata commands, and prepared statements. The tests of the commands that no
  statement of `database/sql` reaches use a session that they drive frame by frame.
- `TestIntegrationAliases`: each alias arrives as the DECIMAL or the DOUBLE that it stands for.
- `TestIntegrationRoundTrip`: `dbimptest.RoundTrip` for each of the 13 types of the type table, with NULL, the smallest and
  the largest value, an empty value, a long value and the smallest step. The ordinary user reads and is refused a write.
- `TestIntegrationLoginRefused`: a wrong password and a wrong user match `dbimp.ErrAuthentication` (D197), and a missing
  privilege does not.
- `TestIntegrationSchemaOfTheDSN`, `TestIntegrationLargeResult` (300000 rows, with pages of 16 KiB and with compression off,
  a second statement between two pages, and a close before the end), `TestIntegrationIntervalOverflow` and
  `TestIntegrationConcurrentQueries`.

The unit tests need no server. `TestContract` runs the cases of `dbimptest.RunContract` against a scripted server. `TestReplayTypes`,
`TestReplayLarge`, `TestReplayPrepared`, `TestReplayCompression`, `TestReplayTimeZone` and the others decode the answers that
`testdata/exasol/` holds. `TestAuthenticationExasol` holds D197 for each recorded refusal.

The tests ran on 2026-10-11 against `exasol-2026.2.0` (the build `2026.2.0-nano.6`) as the administrator and as the ordinary user,
and they passed. The run found the fault of the abort with compression on, which the scripted server did not show (see "Cancellation
and timeouts"). The fuzz test of the DSN ran for 60 seconds and found nothing.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was written on 2026-10-11 from the staged code. A fact of
Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of Exasol from the sections above.

### The server

| | Couchbase | Exasol |
| --- | --- | --- |
| Request | `POST /query/service` over HTTP, with `statement`, `args` and `$name` | One JSON object with `command` in one frame of a WebSocket, and one frame for the answer (Requests) |
| Database | The key `query_context` of the body | The attribute `currentSchema` of the session, set at the login or by `OPEN SCHEMA` |
| Language | SQL++, which is close to SQL | The SQL of Exasol, with scripts, `CONNECT BY`, `PREFERRING` and `QUALIFY` (Statements) |
| DDL | In SQL++ | In SQL, and a transaction undoes it with `ROLLBACK` (Transactions) |
| Parameters | `?`, `$1` and `$name` | `?` only, bound by the server after `createPreparedStatement` (Parameters) |
| Framing | One body for the whole result, which does not page | One frame for each answer. A result of 1000 rows or more has a handle, and `fetch` reads it in pages. The data is a list of columns (Responses) |
| Columns | `signature`, before the first row | `columns` of the result set, before the first row |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | A frame with the status `error` and a five character code. The server computes a whole result before it answers, so no error comes in the middle of the first answer (Errors) |
| Types | JSON. No date, decimal, UUID or binary | 13 types, with a decimal as a number or as a string by its precision, and the dates and the intervals as text (Types) |
| Cancel | The server stops a query when the client leaves | `abortQuery`, which has no answer of its own, and the server also stops the statement of a client that leaves (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | The attribute `autocommit`, and `COMMIT` and `ROLLBACK` as statements. No savepoint and one isolation level (Transactions) |
| Authentication | Basic, or `creds` in the body | A user and a password under an RSA key of the session, or a token (Requests) |
| Default port | 8093, or 18093 with TLS | 8563, with TLS |

The differences that a caller sees:

- The connection is a session, and a statement that is running ends with `abortQuery` when the context ends. The connection stays
  usable (D211).
- Compression is on by default (D214).
- The server binds the arguments, so a statement with arguments costs three requests when it is not prepared (D208).
- The server stores an empty string as NULL (D207).

### The driver

| | `couchbase` | `exasol` |
| --- | --- | --- |
| Size, without tests, on 2026-10-11 | About 1300 lines in 8 files | About 2600 lines in 14 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `User`, `Password`, `Schema`, `NoTLS`, `SkipVerify`, `CertificateFingerprint`, `NoCompression`, `NoAutocommit`, `FetchSize`, `QueryTimeout`, `ClientName`, `Protocol` (D205) |
| Options for one statement | Six `With` options for one statement, and two for `BeginTx`, through `WithOptions` or an argument (D40, D46 and D109) | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithSchema`, through `WithOptions` or an argument (D109). `WithTimeout` aborts the statement. `WithReadonly(true)` gives `dbimp.ErrNotSupported`. `WithDatabase` opens the schema, as `WithSchema` does. `WithParameter` sets any member of the request |
| Arguments | Sent to the server as `args` and `$name` | Prepared on the server and bound there, each in the form of the declared type of its parameter. A named argument is an error (D208) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of the columns of a page, with one decoder for each column, and `fetch` for the next page (`rows.go`, D210) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypeLength` and `ColumnTypePrecisionScale` (D206) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | `bool`, `int64`, `*apd.Decimal`, `float64`, `string`, `dbimp.Date`, `dbimp.LocalDateTime`, `time.Time`, `dbimp.Interval` and `[]byte`, by the type of the column (D206) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `rowCount`. `LastInsertId` gives `dbimp.ErrNotSupported`, because the server has none |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` sets `autocommit` to false, and the end sets it to true. `ReadOnly` and an isolation level give `dbimp.ErrNotSupported` (D209) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | `IsValid` only. The connection holds no state that `database/sql` does not end |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The frame `abortQuery` goes out when the context ends, the driver reads the answer of the aborted statement, and the session stays (D211) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{Code, Message}`, with the five character code of the server, and the constants `CodeConnection`, `CodeTimeout` and `CodeAborted` |
| Authentication | Basic | The password under the RSA key of the session (D205) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Config`, `ParseDSN`, `FormatDSN`, `NewConnector` and the three codes |

The differences that a caller sees:

- A value has the Go type of its column, and a DECIMAL is an `int64` or an `*apd.Decimal` by its precision (D206).
- A statement with arguments is prepared on the server, where Couchbase sends the arguments with the statement (D208).
- `LastInsertId` fails, and `WithReadonly(true)` and a read-only transaction fail (D209).
- A transaction does not start a statement on the server. It turns autocommit off, and the server starts the transaction at the next
  statement (D209).
- Rows hold the context of the query, and so do a statement and a transaction (D215), where Couchbase holds one in its transaction only.
- The default of `protocol` and the wait after an abort are choices of the driver that no decision of Ken names (see "Open questions").
