# Exasol

This file holds what is known about Exasol over its WebSocket API, for a possible driver
`exasol` (W44 and D200). The headings are the template of [DRIVER.md](DRIVER.md). A fact is
"recorded", with the name of its request in quotes, "measured", with the date, or "not
measured", with its source.

## Summary

Step 2 measured this on 2026-10-11 against `exasol-2026.2.0`, which `dbrun` started as
`nano.6` on a random port. The probe was a throwaway program that used the WebSocket
client of the root package (D200). The facts are "measured" unless a source is named.
`exasol-driver-go` is the second source, read in place and never imported (D29). The
documents of the WebSocket API at `github.com/exasol/websocket-api` were not read. The
measurements and `exasol-driver-go` agree on every point below.

### R, H and S

- R passes. `dbrun start exasol-2026.2.0` started one container. It listens on
  port 8563 inside, and the user is `sys`.
- H passes with a change in meaning, which D200 item 1 allows. The query goes in
  a JSON object, in one text frame of a WebSocket, and the answer is one JSON text
  frame. No frame is binary unless the client turns on compression. See below.
- S passes. The query is Exasol SQL, and a person can type it.

### The conditions of "When it cannot be a driver"

- The columns come before the first row. Every result set holds `columns`, with the
  name and the type of each column, in the order of the statement, even for a result with no
  row in the first message. Passes.
- The result can be read in parts. A result set larger than one message has a
  `resultSetHandle`, `numRows` for the whole result and `numRowsInMessage` of 0 (measured
  for 300000 rows). The `fetch` command reads from `startPosition`, and `numBytes` bounds the
  size of one answer. A fetch of 2000 bytes at position 1000 gave 91 rows. Memory
  is bounded by `numBytes`, as long as the client chooses it. Passes.
- The server does not cut a result short. A fetch at the position `numRows` is an error
  (`Invalid result set seek`), and a fetch after `closeResultSet` is an error
  (`Resultset handle 1 is unknown`). Not measured: how long an open handle lives,
  and the limit of open handles in a session.
- No package is needed. The login encrypts the password with RSA PKCS1v1.5, and
  `crypto/rsa` does it. Compression is `compress/zlib`. Passes.

So Exasol can be a driver, and the client of the WebSocket is the only new
code that the root package needs.

### The protocol

A session is one WebSocket. The client sends one request, and the server sends one answer
for it. Each answer is `{"status":"ok","responseData":{...}}`, or `{"status":"error",
"exception":{"text":...,"sqlCode":"42000"}}`. An answer can also hold `attributes`, which are
the session attributes that the statement changed, such as `currentSchema`.

- The connection. `wss://host:8563`. The server uses TLS with a certificate that nothing
  trusts, so a client that checks it needs `validateservercertificate=0`, as `dbmeta` writes
  it. A plain `ws://` upgrade succeeds, but the login then answers `Only TLS
  connections are allowed.` with the code `08004`. The server chose no extension and no
  subprotocol, and the client offered none.
- The login. `{"command":"login","protocolVersion":3}` returns `publicKeyModulus` and
  `publicKeyExponent`, both in hex. The client then sends a message with no `command`
  key: `username`, `password` (the password encrypted with that key, in Base64),
  `useCompression`, `clientName`, `driverName` and `attributes`. The answer holds
  `sessionId`, `releaseVersion`, `maxDataMessageSize` (67108864), `identifierQuoteString`
  and the time zone. The server accepted `protocolVersion` 3 and 4. A wrong password
  answers `Connection exception - authentication failed.` with `08004`, after 2.3
  seconds. `exasol-driver-go` also has `loginToken`, with an access token or a refresh
  token. Not measured.
- A query. `{"command":"execute","sqlText":"..."}` returns `results`, a list with one
  entry for each statement, each `resultSet` or `rowCount`. `executeBatch` takes `sqlTexts`
  (measured with two `SELECT`). A result set has `columns` and `data`, and `data` is
  a list of columns, each a list of the values of that column. A small result is in the
  answer. A large one has a handle, as above.
- Parameters. `createPreparedStatement` returns `statementHandle` and `parameterData.columns`.
  `executePreparedStatement` sends `columns`, `numRows` and `data` in the same
  column form, so it can bind many rows in one request. In the measurement the server
  typed each `?` as `VARCHAR(2000000)` and refused a JSON number for it
  (`getString: JSON value is not a string`), so every argument goes as a string
  or as null. Then `closePreparedStatement`.
- Transactions. `setAttributes` with `{"autocommit":false}` starts a transaction. The
  attribute `openTransaction` was 1 after an `INSERT`. Then the statements `ROLLBACK` and
  `COMMIT` end it (measured: the insert was gone after `ROLLBACK`).
- Cancel. `{"command":"abortQuery"}` sent while a query runs makes that query answer
  `Client requested execution abort.` with `R0003`. The abort has no answer of its own.
  The session worked after it. The attribute `queryTimeout` makes the server end
  a query with `R0001` after that many seconds (measured: 1 second).
- Close. `closeResultSet` with `resultSetHandles`, and `disconnect`.
- Errors. The code is `sqlCode`, a string of five characters such as `42000`, and
  the text ends with the session number.
- Compression. With the attribute `compressionEnabled` true at the login, every
  message in both directions is zlib data in a binary frame, and the default is
  text. This is not the `permessage-deflate` extension of the WebSocket. The
  client of the root package needs no extension for it. Measured: a 5-row
  result was 229 bytes on the wire.

### The types

Measured with one `SELECT`. `DECIMAL(18,0)` is a JSON number, and `DECIMAL(10,2)` (`"1.5"`)
and `DECIMAL(36,0)` are JSON strings. The exact rule is not measured. The number 9007199254740993 came back exact, so the
driver must read a number as text and never as `float64`, which is why
`exasol-driver-go` has a workaround for issue 113. `DOUBLE` is a number, `1e300`
included. `DATE`, `TIMESTAMP` (6 digits of fraction), `INTERVAL DAY TO SECOND`,
`INTERVAL YEAR TO MONTH`, `HASHTYPE` and `GEOMETRY` (WKT) are strings. `CHAR` is padded.
A NULL is `null`. The type of each column is in the `dataType` object: `type`, and as the
type needs them, `precision`, `scale`, `size`, `characterSet`, `fraction` and `srid`.
Step 8a decides the mapping.

### The verdict

Exasol meets R, H and S, and it meets no condition of "When it cannot be a driver".
It can be a driver.

### The risks

- H is a WebSocket, not plain HTTP. D200 allows it, and Ken decides if this changes the scope.
- The client reads a whole message and then decodes it. A message is at most
  `maxDataMessageSize`, 64 MiB, and `numBytes` of `fetch` keeps it smaller. The first
  result of an `execute` has no such bound: a result that fits in one message comes
  whole in it. Not measured: the size at which the server switches to a handle.
- One request is in flight on a session. A `Close` or a cancel from another goroutine needs
  a second request on the same socket, so the driver must serialize its writes and
  read the answer of the aborted query, never the answer of the abort.
- The recorder records HTTP only. Recording a session means recording its messages in
  order, and the login holds a password that is encrypted with a key that changes on
  each login, so a replay cannot compare it (D200 item 3).
- Not measured: the loss of a session when its socket is idle, a ping from the server, the
  maximum number of sessions, and the access-token login.

## Requests

Not written yet.

## The DSN

Not written yet.

## Responses

Not written yet.

## Types

Not written yet.

## Parameters

Not written yet.

## Transactions

Not written yet.

## Errors

Not written yet.

## Cancellation and timeouts

Not written yet.

## Statements

Not written yet.

## Principals

Not written yet.

## Flavors

Not written yet.

## Interfaces

Not written yet.

## Faults

Not written yet.

## Second opinions

Not written yet.

## Open questions

Not written yet.

## Integration tests

Not written yet.
