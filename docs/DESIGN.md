# Design

This file holds the design of the code that every driver shares. It is W4
in [BACKLOG.md](BACKLOG.md). The root package `dbimp` holds the code that a
driver uses at run time (D4). The package `dbimptest` holds the code that a
driver uses in its tests (D37). [DRIVER.md](DRIVER.md) says when a driver
uses each part.

## The root package

The root package imports the standard library and `apd`, and nothing else
(D13). Each file holds one part.

| File | Holds |
| --- | --- |
| `errors.go` | `Error`, the sentinel errors, and `StatusError` with `CheckStatus` |
| `url.go` | `ParseURL`, `NewQuery` and `Query`, for the DSN |
| `http.go` | `NewTransport`, `NewClient` and `Send` |
| `auth.go` | `AuthBasic`, `AuthBearer`, `Query.Auth` and `SetAuth`, which send the secret of a DSN as the key auth says (D94 and D116) |
| `sigv4.go` | `SignV4`, which signs a request with AWS Signature Version 4, for DynamoDB and for the recorder. A caller that sends temporary credentials sets `X-Amz-Security-Token` first, and the signature covers it (D169) |
| `stream.go` | `Stream` and `NewStream`, which read the body of a response |
| `rows.go` | `ObjectRows`, `NewObjectRows`, `ContinueObjectRows`, `ArrayRows` and `NewArrayRows`, which read rows by D18 |
| `values.go` | `IsNull`, `Int64`, `Float64`, `Bool`, `String`, `Decimal`, `Number` and `Any`, which turn a JSON value into a Go value, and `Assign` |
| `civil.go` | `Date`, `LocalTime`, `OffsetTime`, `LocalDateTime`, `Interval` and `Vector`, the types of D138 and D139, with a `Parse` function for each of the first five |
| `version.go` | `IsVersionQuery` and `NewVersionRows`, which a driver uses to answer `SELECT version()` when its product has no query for the release (D181) |
| `placeholder.go` | `Syntax` and `Placeholder`, which find placeholders and bind arguments (D34) |
| `options.go` | `Option`, `WithOptions`, `IsOption`, `Resolve`, `Unsupported` and `MarshalParams`, the machinery of the options of every driver (D109) |
| `cbor.go` | `CBORDecoder`, `NewCBORDecoder`, `CBOREncoder`, `CBORHead`, and `CBORMajor` with its constants, which read and write CBOR (D49) |
| `websocket.go` | `DialWebSocket`, `WebSocket`, `WebSocketOption` with its `WithWebSocket` functions, `MessageType`, `CloseCode` and `CloseError`, a client for WebSocket (D200) |

### Errors

A sentinel error is a constant of the type `Error` (D6). A driver wraps one
with `%w`, and a caller tests it with `errors.Is`:

- `ErrNotSupported` for a feature that the database does not have, such as a
  transaction (D20).
- `ErrScheme`, `ErrUnknownKey`, `ErrRepeatedKey` and `ErrInvalidValue` for a
  DSN (D27 and D35).
- `ErrExtraColumn` and `ErrColumnCount` for a row (D18).
- `ErrIncomplete` for a result set that failed, or that the server cut
  short, after at least one of its rows reached the caller (D21 and D107).
- `ErrUnterminated` and `ErrArguments` for placeholders (D34).
- `ErrAuthentication` for a server that refused the credential: a wrong
  password, key or token. The error of each driver matches it with
  `errors.Is`, through an `Is` method that reads the fields of the server
  (D197). It never matches a refusal for lack of a permission. A `StatusError`
  with the code 401 matches it, and a `StatusError` with the code 403 does not,
  because a server sends 403 for both causes, so the driver decides it.

`CheckStatus` turns a response whose status is not 2xx into a `StatusError`.
It keeps the code and at most 64 KiB of the body, so that a driver can read
the error that its product writes there. HTTP 429 and HTTP 503 are a
`StatusError` like any other, and nothing retries them.

### The DSN

`ParseURL(name, dsn)` parses the DSN with `net/url`, and refuses a scheme
that is not the one name of the driver (D27 and D35). Its error never holds
the DSN, because the DSN can hold a password. It also refuses a host with a
colon that is not an IPv6 address. `net/url` reads the host `::` as `:`,
which no URL can write back. The fuzz test of the SurrealDB DSN found it,
and the Couchbase DSN of `v0.1.0` took it too.

`NewQuery(u, known...)` reads the query of the URL. It refuses a key that is
not in `known` and a key that appears twice, so each value that `Query`
returns is the only value of its key. `Query` reads a string, a bool, an int
and a duration, each with a default.

### HTTP

`NewTransport` returns the transport for one connector. It takes a proxy
from the environment, bounds the dial and the TLS handshake, and asks for
gzip, which it decompresses itself. It sets no timeout for the headers of a
response, because some servers send them only after a long query ends, and
such a timeout cuts the query short. The context of each request sets its
deadline.

`NewClient` returns a client with no `Timeout`. If the driver does not
follow redirects, the client returns a redirect as the response. A driver
decides that in step 9 of [DRIVER.md](DRIVER.md).

`Send` sends a request once (D8). A trace records whether the client got a
connection. If it did not, as when the dial or the TLS handshake failed,
the error wraps `driver.ErrBadConn`, so that `database/sql` tries another
connection. If the context ended, the error wraps the error of the context,
and never `driver.ErrBadConn`. In every other case, the request can have
reached the server, and the error is a plain error. `net/http` removes the
password from the URL in its errors.

The transport of `net/http` sends a request again by itself only when it
wrote nothing on a connection that it had reused, which is safe. It never
sends a `POST` again after it wrote it.

### Reading a result

`Stream` holds the body of a response and a `jsontext.Decoder` that reads
from it (D36). A driver reads the response through `Decoder`. When it has
read the last value, it calls `End`, which returns an error if anything but
white space follows. `Close` closes the body and reads nothing more, so a
result closed early is never drained. On HTTP/1.1 that closes the
connection, and on HTTP/2 it resets the stream.

A token from the decoder is void after the next call to the decoder. So a
driver reads the text of a name before it reads the value of the name. The
first version of `Any` read the name after the value, and a test found it.

`ObjectRows` reads an array of objects by rule 2 of D18. If the driver
passes no columns, it reads the first object to learn them, and `Next`
returns that object first. `NewObjectRows` reads the `[` that starts the
array. `ContinueObjectRows` is for a driver that has read the `[` itself,
such as to look at the kind of the first row before it chooses how to read
the rows. A key that a later object lacks is a nil value,
and a JSON null is the value `null`, so a driver can tell the two apart
when its product has both. A key that only a later object has is
`ErrExtraColumn`.

`ArrayRows` reads an array of arrays by rule 1 of D18, when the server names
the columns elsewhere in the response. A row with the wrong number of values
is `ErrColumnCount`.

Both read one row for each call of `Next`, and copy each value, because the
decoder reuses its buffer. The code that reads the rest of the response,
such as an error after the rows, belongs to the driver, because each product
writes it in its own way.

### Values

These functions read the text of one JSON value. None of them passes a
number through float64 unless the driver asks for a float64, or calls
`Number` or `Any` on a number with a fraction or an exponent (D19):

- `IsNull` is true for a missing value and for `null`.
- `Int64`, `Float64`, `Bool` and `String` read a value of that kind.
- `Decimal` reads a number, or a string that holds one, as a new
  `*apd.Decimal` (D33).
- `Number` reads a number that has no type from the server. An integer that
  fits is an `int64`, a larger integer is an `*apd.Decimal`, and a number
  with a fraction or an exponent is a `float64`.
- `Any` reads a whole document by rule 3 of D18, as `nil`, `bool`, `string`,
  a number as `Number` reads it, `[]any` or `map[string]any`.

A decimal is an `*apd.Decimal`, and a new one for each value. An
`apd.Decimal` holds a pointer to its digits once they are large, so a copy
of the value shares them with the original.

`Assign` is for the `ScanColumn` method of `driver.RowsColumnScanner`. It
stores a value in a `*any` as it is, and an `*apd.Decimal` in an
`*apd.Decimal`. It hands every other pair to `sql.ConvertAssign`, and it
hands a decimal as text, because a `*string`, a `*float64` and the `Scan`
method of `apd.Decimal` take text. So `sql.Null[apd.Decimal]` works, a NULL
in a `*string` is the error of `database/sql`, and a NULL in a
`sql.Null[T]` is not valid.

### The version of the server

A driver answers `SELECT version()` itself only when its product has no SQL
or query that returns the release (D181). `IsVersionQuery(query)` is true for
that statement. It ignores case, the white space around the statement, and
one final semicolon. White space must separate `SELECT` from `version()`, and
none stands inside the parentheses. It accepts no argument, no comment and no
other text, and it parses nothing, so any other statement goes to the
product. `NewVersionRows(release)` returns the result as a `driver.Rows` with
one row and the one column `version`, which holds `release` as the product
writes it. The driver reads the release from the endpoint or the header that
carries it, and a user that the product refuses gets the error of that
request. The driver calls the two functions in `QueryContext`, and never in
`ExecContext`.

### The types that Go lacks

`civil.go` holds the types of D138 and D139, for the kinds of
[TYPES.md](TYPES.md) that Go has no type for. `Date`, `LocalTime` and
`LocalDateTime` have no zone. `OffsetTime` is a `LocalTime` and an offset in
seconds, with no date. `Interval` keeps months, days and nanoseconds apart,
as `pgtype.Interval` of pgx and the Arrow type `Interval(MonthDayNano)` do.
`Vector[T]` is a slice of `int8`, `int16`, `int32`, `int64`, `float32` or
`float64`, which a driver sends as a vector, where a plain slice goes as a
list.

- `String` writes ISO 8601, and each `Parse` function reads it back. A year
  of more than four digits has a sign, as Neo4j writes it. `ParseInterval`
  takes a sign on each part, and on the whole, such as `P-1Y-2M` and
  `-PT1.5S`, with each unit once and in its order.
- `DateOf`, `LocalTimeOf` and `LocalDateTimeOf` take the parts of a
  `time.Time`, and `In` gives the `time.Time` of a value in a location.
- Each type is a `driver.Valuer` of its text, so a driver that does not name
  the type sends the text. A driver that names it keeps it in
  `CheckNamedValue`, and writes it in the form of its server.
- `Assign` stores a value in a destination of its own type, or its
  `sql.Null`. It gives a `*time.Time` or an `sql.Null[time.Time]` the value
  in UTC, on 0000-01-01 for a `LocalTime`, and an `OffsetTime` on 0000-01-01
  in a zone of its offset. Any other destination gets the text of `String`.
  A `Vector` is a slice, so `database/sql` assigns it to a slice of its
  elements.

### CBOR

`cbor.go` reads and writes CBOR (RFC 8949), for a driver whose server speaks
it. Ken approved CBOR for SurrealDB, written here with the standard library
only (D49 and D13). The code knows the major types, and gives a tag no
meaning, because each product gives its tags their own meaning.

`CBORDecoder` reads one item at a time, as `jsontext.Decoder` does, so a
driver never holds a result in memory (D25). `PeekHead` and `ReadHead` read
the head of an item. `ReadString` reads a string, and each chunk of a string
of indefinite length. `More` reports whether an array or a map holds another
item, and reads the break of one of indefinite length. `ReadRaw` returns the
bytes of one whole item, which a driver keeps as the raw value of a column
and decodes when it is scanned. `Skip` reads one item and keeps nothing.

The decoder refuses an item nested deeper than 256 levels, and it grows a
string as its bytes arrive, so a hostile length or a hostile nesting costs
no memory and no stack. A fuzz test holds that `ReadRaw` reads back what it
returns.

`CBOREncoder` writes each head in its shortest form, and a float as 64 bits.
The driver writes its tags with `Tag`.

### Placeholders

`Syntax` says how a product writes literals, quoted identifiers and
comments. `Placeholders` finds each `?` and each `@name` outside them. An
`@` that another `@` follows, as in `@@version`, is not a placeholder. A
statement that ends inside a literal or a comment is `ErrUnterminated`.
For AQL, `SlashComments` skips a `//` comment, `DoubleAt` returns `@@name`
as a placeholder whose `Double` is true, and `DigitNames` takes a name such
as `@1` (D104).

`Bind` writes each argument into the statement with a function that the
driver supplies for the literals of its product (D34). A `?` takes the next
argument that has no name, and an `@name` takes the argument with that
name. A missing argument and an argument left over are both
`ErrArguments`. A fuzz test holds the parser.

### The WebSocket client

`DialWebSocket(ctx, url, opts...)` opens a WebSocket (RFC 6455) for a database whose
server takes its requests as messages, such as Exasol (D200). It sends the
request for the upgrade with `Send`, so the TLS configuration and
`driver.ErrBadConn` for a failed dial work as they do for HTTP. The default client turns
HTTP/2 off, because a WebSocket cannot start over HTTP/2. It also uses no proxy and does
not read the proxy of the environment, because a plain HTTP proxy cannot carry the upgrade
of a `ws` URL. `WithWebSocketProxy` sets a proxy function, and a caller that passes its own
client with `WithWebSocketClient` sets the field of its transport (D217). A `Host` header
of the caller sets the host of the request. The context bounds the dial, and the connection
outlives it. A dial with the default client closes the idle connections of its own
transport on every exit.

`Read(ctx)` returns one whole message, text or binary, and `Write(ctx, type, data)`
sends one. `Read` joins fragments, answers a ping with a pong, and drops a pong.
After the server sent a close frame, `Read` answers it and returns a `*CloseError`
that holds the code and the reason. A server that ends the TCP connection at the start of a
frame, with no close frame, gives an error that wraps `io.EOF`, and an end inside a frame
gives `io.ErrUnexpectedEOF`. A message over the limit, 64 MiB by default
(`WithWebSocketMaxMessage`), closes the connection with the code 1009, and so does a message
of more than one million frames. The buffer of a message grows only as bytes arrive, by at
most 1 MiB for each step, so a frame that declares a large length and sends little holds
little memory. The client masks each frame that it sends.

The context of a call works like this (D217). If it is done before the call starts, the call
returns its error and the connection stays open. If it ends while the call waits for the
network, the call sets a deadline in the past on the TCP connection that carries the upgrade,
which the dial records with the `GotConn` hook of `httptrace`. The call then closes the
connection and returns the error of the context, because the stream of frames cannot continue
after a call that stopped half way. The body of the response of `net/http` has no deadline, so
the client never relies on it. If the context ends after the call did its work, the call
returns its result, and the client removes the deadline, so the next call with a new context
works. The client never closes the connection for a call that returned. `Close` sends a close
frame and waits at most five seconds for the reply. `Close(CloseNoStatus, "")` sends a close
frame with no code, and `Close` refuses a code that RFC 6455 does not let an endpoint send
(1004, 1006, 1015, 1016 to 2999 and from 5000) with an error that wraps `ErrInvalidValue`, and
it sends nothing.

The client does not do these things:

- It is a client only. There is no server.
- It offers no extension, so there is no deflate of messages. A server that
  chooses an extension anyway is `ErrHandshake`.
- It does not stream. `Read` holds a whole message in memory, up to the
  limit, and `Write` sends one frame.
- It sends no ping by itself, and it does not reconnect.

The tests run a server that the test file writes with `net/http` and a hijack.
The tests of the contexts also run over a real TCP connection whose body has no deadline,
because `net.Pipe` has a deadline and hides the fault of D217. `FuzzWebSocketFrame` holds the
reader of frames.

## The package dbimptest

Only a test, and the command `dbimptest/cmd/record`, import `dbimptest`. It imports `testing` and
`net/http/httptest`. Every driver imports the root package, so if the root
package held these helpers, every consumer of a driver links both packages
for code that only a test uses. So they are not in the root package (D37).

| File | Holds |
| --- | --- |
| `exchange.go` | `Exchange`, `Request`, `Response`, `ReadExchange`, `Match`, `DefaultMatch`, `Replay` and `ReplayRelease` |
| `manifest.go` | `Manifest`, `Entry`, `ReadManifest`, `WriteManifest`, the file names `ManifestName` and `RequestsName`, and the principals `Administrator` and `Ordinary` |
| `record.go` | `Recorder`, `NewRecorder` and `WithLabel`, which write the exchanges of step 6 |
| `websocket.go` | `WebSocketExchange`, `WebSocketFrame`, `NewFrame`, `ReadWebSocketExchange`, `WebSocketMatch`, `WebSocketTurn`, `DefaultWebSocketMatch`, `WebSocketMatchOn`, `ReplayWebSocket`, `RedactMembers` and the methods `WriteWebSocket` and `RedactMembers` of `Recorder` |
| `websocket_script.go` | `ScriptedWebSocket`, `ServeWebSocketScript` and `WebSocketStep`, a server that answers the frames of a connection with a script |
| `cmd/record/` | The command that records the requests of `requests.json` |
| `goroutines.go` | `CheckGoroutines` |
| `heap.go` | `HeapGauge`, `NewHeapGauge` and `HeapLimit`, which measure the growth of the live heap while a driver reads a result of 64 MiB (D25) |
| `contract.go` | `Contract`, its cases `ColumnsCase`, `NullCase`, `ErrorCase` and `StreamCase`, and `RunContract` |
| `contract_websocket.go` | `WebSocketContract` and its cases, which `RunContract` runs when `Contract.WebSocket` is set |
| `tables.go` | `TypeTable`, `TypeRow`, `Kinds`, `InterfaceTable`, `WriteBlock`, `EnvUpdate` and the markers of the tables |
| `features.go` | `Features`, `Source` and `Feature`, the survey of step 5a, with `FeaturesName`, the constants of a kind, a source and a verdict, and `ReadFeatures` |
| `roundtrip.go` | `RoundTrip`, `RoundTripCase` and `Value`, the round trip of one type for step 14a |

### Recorded exchanges

Each file under `testdata/<driver>/`, except `manifest.json`,
`requests.json` and `features.json`, is one `Exchange`: the method, the path, the query, the headers and the body of a
request, and the status, the headers and the body of its response. A body
that is text is in `body`. A binary body, such as one in CBOR, is in
`binary`, which the file holds as base64, and `Content` returns either.
If the server closed the connection before the end of the body, as
InfluxDB 3 does for an error after some rows, `truncated` is true and the
body holds what arrived. `Replay` then sends that part and closes the
connection, so the driver reads `io.ErrUnexpectedEOF` as it did from the
server.

`Recorder` is an `http.RoundTripper` that writes each exchange with a real
server. It names each file for the release, a number and the request. It
takes the item of step 6 and the principal of an exchange from the context
of its request, which `WithLabel` sets, or else from the last call of
`Label`. The context keeps the label right for a request that runs in the
background while others are sent.

The command `dbimptest/cmd/record` reads `testdata/<driver>/requests.json`,
sends each request in it as both principals through a `Recorder`, or as the
administrator only when `-ordinary` is empty, and writes the
exchanges and the manifest. A request sends its `body` as JSON, or as CBOR
when its `encoding` is `cbor`, or its `text` as plain text. The script can
name headers that every request of one principal sends, in `header`, such as
the headers that say where a SurrealDB user is defined. A request keeps a
value of its response with `capture`, such as the id of a transaction of
Neo4j, or a number, such as the id of a statement of Avatica, and a later
request of the same principal writes it into its body, its path or a header
as `{{name}}`. A path that starts with `header:`, such as
`header:X-Trino-Started-Transaction-Id`, keeps a header of the response, and a
response with no body or with no member at that path keeps none. A `path` or a
followed address that is an absolute URL, such as the `nextUri` of Trino, goes
to the server of the script with the path and the query of that URL. A page
that a request follows carries the same credentials as the first request: basic
authentication, a Bearer token or a Signature Version 4 signature. A kept
object, such as the statement handle of Avatica, takes the place of the whole
string `"{{name}}"` in a body, as JSON. A body can name `{{user}}` and `{{password}}`, the credentials
of the URL of the principal, for a server that takes them in the body, such
as the `info` of `openConnection` of Avatica, and the recorder writes each
password in a body as `REDACTED`. A request with `releases` runs only on a release
whose name starts with one of them, such as `influxdb-3`, as `principals`
limits a request to some principals. A request of the setup or the teardown
with `"server": "second"` goes to the URL of the flag `-second`, such as the
Controller of Pinot, through which the setup makes the tables that the
Broker cannot. A script with `"auth": "bearer"` sends the password of each
URL as a Bearer token, for a server that takes a token and no user, such as
libSQL with a JWT (D94). A script with `"auth": "cosmos"` signs each request with
the master key of Azure Cosmos DB, which is the password of each URL, and the
flag `-insecure` accepts the certificate that the emulator of Cosmos DB makes
for itself. A script with `"auth": "sigv4"`, a `region` and a
`service` signs each request with AWS Signature Version 4, with the user of
each URL as the access key and its password as the secret key, as DynamoDB
takes them. It signs the host, `X-Amz-Date`, each header that the script
sets, and the body, with `dbimp.SignV4`. A request with `"auth": "none"` sends no credentials at all, and a request
with `"auth": "wrong"` sends a password that differs from the one of the URL. It replaces the files and the entries that an
earlier run wrote for the same release. [DRIVER.md](DRIVER.md) shows how to run
it in step 6. The gates and `Replay` skip `requests.json`. The recorder writes
the value of `Authorization`, `Cookie` and `Set-Cookie` as `REDACTED`.
`WriteManifest` adds what it recorded to `manifest.json`.

`Replay` starts a fake server that answers each request with the response of
the first exchange that matches it. `DefaultMatch` compares the method, the
path, the query, and the body, where two JSON bodies match if their
canonical forms are equal. A driver whose requests hold a value that changes
each time, such as an id for the request, supplies its own `Match`. A
request that nothing matches fails the test. `ReplayRelease` does the same
with the exchanges of one release, for a test of what that release sends,
such as the order of the columns on 7.2.9.

### WebSocket exchanges

A database that takes its requests as messages of a WebSocket, such as Exasol (D200), has
exchanges of a different form. `dbimptest` records and replays them (D202 item 2), and it
holds no name of a database.

The file. An exchange of a WebSocket is one request frame and the reply frame that
the server sent for it. It is one JSON object in its own file under `testdata/<driver>/`,
named as an HTTP exchange is named, and the manifest names it as it names any file. The
type is `WebSocketExchange`. The member `transport` is `websocket`, which is how a reader
tells the file from an `Exchange`. An `Exchange` file has no such member, and `ReadExchange`
and `Replay` read it as before. `Replay` skips a file with the transport `websocket`.

```json
{
  "transport": "websocket",
  "session": "main",
  "request": {"type": "text", "text": "{\"command\":\"execute\",\"sqlText\":\"SELECT 1\"}"},
  "reply": {"type": "text", "text": "{\"status\":\"ok\",...}"}
}
```

A frame is a `WebSocketFrame`. Its `type` is `text`, `binary` or `close`. A text frame holds
its message in `text`, or in `json` as a JSON value if the message is compact JSON of at most
4096 bytes, which keeps the file readable. `Content` returns the message in either form, so a JSON
value that the file indents is compact again. A binary frame holds it in `binary`, as base64. A close frame holds the code in
`code` and the reason in `text`, and only a reply can be one. The member `zlib` is true
for a binary frame whose bytes are the zlib form of `text`. The file then holds the readable
text, `Replay` compresses it for the client, and the matching sees the text. A frame that is
binary and not `zlib` is compared as bytes. `session` is the name of the session that the
exchange belongs to. A request with no `reply` is an exchange that the server did not answer,
such as a message that cancels a query.

The script. A script of `requests.json` that names `"websocket": {"path": "/"}` at the
top is a script for a WebSocket. Each request is then a frame and no HTTP request. The
members are:

- `frame`: the JSON value to send as one text frame. With `"binary": true`, it is a string of
  base64 that the recorder sends as one binary frame. With `"zlib": true`, the recorder sends
  the zlib form of the frame as a binary frame, and it reads a binary reply as zlib data
  when it can. The reply is then recorded as text, with `zlib` true.
- `session`: the name of the session. The default is `main`. A request with a name that is
  new opens another WebSocket, so a script can hold two sessions, such as a session
  that waits and a session that cancels. A request with `"close": true` and no frame closes
  the session.
- `capture`: the same as for HTTP. A path such as `responseData.sessionId` names a member
  of the JSON of the reply. A later frame of the same principal writes the value into
  `{{name}}`, escaped as the text of a JSON string, so the frame stays valid JSON. The text
  `"{{name}}"` of a captured object takes the place of the string as JSON, and the text
  `"{{#name}}"` takes the place of the string as the bare text of the value, so a captured number,
  such as a handle, is a JSON number. `{{user}}` and `{{password}}` are the credentials of the URL
  of the principal. `"auth": "wrong"` makes `{{password}}` a wrong password. A capture that finds
  nothing, such as the handle in an error reply, only prints a line, and a later frame that uses
  the name then carries the text of the name, as a caller with no handle would.
- `encrypt`: `{"member": ..., "modulus": ..., "exponent": ...}` replaces a string member of the
  frame with its RSA encryption, with the padding of PKCS #1 version 1.5, in Base64. The key is
  the two captured values, in hex, that `modulus` and `exponent` name. It is for a login that
  sends the password under a public key that the server gave in an earlier reply. The
  recorder then redacts the member, because the text differs on each run.
- `text`: a text frame that is sent as it is, in place of `frame`, for a message that is not JSON.
- `noReply`: the recorder sends the frame and reads no reply.
- `interrupt`: a frame, and a wait in `after`, such as `abortQuery` one second after a long
  query. The recorder sends the interrupt on the same session while it waits for the reply. It
  writes the interrupt as its own exchange, with no reply, before the exchange that it
  interrupted.
- `timeout`: how long to wait for the reply. The default is one minute, and a read that ends
  by the timeout is an error of the recording. With `timeout` set on the request, silence is the
  result: the recorder writes the exchange with no reply, and the read closes the session.
- `wait`, `principals`, `phase`, `releases`, `item` and `name` mean what they mean for HTTP.

A reply that is a close frame is recorded with its code. A connection that ends with no close
frame is recorded as a close reply with the code 1006, which RFC 6455 keeps for that case, and the
session is dropped. `ReplayWebSocket` ends such a connection with no close frame. A script that
closes a session that is already gone prints a line and goes on, because the server can end a
session by itself.

The top of the script has `redact`, a list of names of members. Wherever a name of the list
is a member of a JSON object in a frame, at any depth, the recorder writes its value as
`REDACTED`. It does so in the request and in the reply, as it does for the header
`Authorization`. The recorder also writes each password of the URLs as `REDACTED`, as it does
for HTTP. A frame that is binary and not `zlib` cannot be redacted.

The recorder dials with `dbimp.DialWebSocket`, with the flag `-insecure` for a certificate
that nothing signed, sends each frame in order, and records the reply. `Recorder` has the
method `WriteWebSocket` that writes one exchange, so it numbers the files, makes the
entries of the manifest and writes the files as it does for HTTP.

The replay. `ReplayWebSocket(t, dir, match)` starts a TLS server that accepts a WebSocket on
any path. For each request frame that arrives, it sends the reply of the first exchange of
`dir` that `match` accepts, in the order of the file names. A request that no exchange
matches fails the test and closes the connection. An exchange with no reply sends nothing,
and a reply that is a close frame closes the connection. The server answers a ping and the
close of the client. A client trusts the certificate of `srv.Certificate()`, or turns the
check off. `Replay` for HTTP stays as it was.

A `WebSocketMatch` gets a `WebSocketTurn`: the frame that arrived (`Got`) and the one before
it on the same connection (`Prev`, empty for the first), and the exchange that is
tried (`Exchange`) with the one before it in its session (`Before`, nil for the first).
`DefaultWebSocketMatch` compares the key of two frames. The key of a JSON frame is the string
member `command`. A frame with no such member, such as the message that carries the
credentials of a login in Exasol, takes the key of the frame before it, with the prefix
`after:`, so it matches the exchange that follows the same command. A frame that is not JSON
has the key of its bytes. `WebSocketMatchOn("sqlText")` is a `WebSocketMatch` that also
compares the members it names, for a test that sends several statements with one command.

### The scripted server and the contract

`ServeWebSocketScript(t, script)` starts a TLS server for a case that no recording holds, such as an answer that
waits for a cancel. It matches nothing. Step i of the script answers the i-th request frame of each connection, so
the script lists the frames that a correct driver sends, in order. A `WebSocketStep` holds the `Reply` frame, and
three flags: `Hold` makes the server wait for one more frame before it replies, as a server does while a query runs and
the client sends `abortQuery`, `Drop` makes it read the request and close the connection with no reply, and `Mute` makes it
read the request and stay silent until the client closes the connection. A request frame after the last step fails the test.
`Frames` returns the frames that the server read, with the compression undone, so a test sees what the driver sent. `Held`
returns a channel that closes a moment after the server starts to wait at a step with `Hold`, so a test cancels a context at that time.

`RunContract` sends HTTP, and a driver of a WebSocket cannot run its cases. `Contract.WebSocket`, a `WebSocketContract`, makes
`RunContract` run the cases of D8 and D18 to D21 against a scripted server instead, and ignore the other fields. The driver supplies
the steps of each case, because each product has its own frames: the columns in order, a NULL, an error after some rows, a close
before the end that reads no more rows, a cancel while the server holds the request that reads more rows, a request that is sent once,
a server that is not there, and a transaction that is never faked. Each case calls `CheckGoroutines`, so the test that calls
`RunContract` must not run in parallel with another test. The Exasol driver is the first to use it.

The limits. The matching is by frame, so it cannot tell two frames of one command that
differ in a member that no `Match` compares. The first exchange that matches always answers,
so a test that needs the second answer to one request supplies a `Match` that sees the
members. The server does not stream, does not fragment, and does not offer an extension.

### The manifest

`manifest.json` names the driver and has one entry for each recorded
exchange. An entry holds the item of step 6, the principal, the release as
`dbrun` names it, the date, and the file. An item that does not apply to the
product, such as transactions for a product that has none, has an entry with
a reason in `absent` and no file. If the product has no ordinary user,
`noOrdinaryUser` holds the reason, and only the administrator needs entries.

### The contract

`RunContract` tests the part of D8 and D18 to D21 that every driver keeps in
the same way. The driver supplies a function that opens it against a URL,
and the bodies of the results that it needs, in the form of its product.
Each subtest starts a fake server:

1. The columns keep the order in which they arrive.
2. A NULL scans as nil into a `*any`, as not valid into a
   `*sql.Null[string]`, and as an error into a `*string`.
3. An error after some rows reaches `Rows.Err`, after exactly those rows.
4. `Rows.Close` after the first row of a result of 64 MiB reads nothing more.
   The test fails if the server sent more than half of the result.
5. A context cancelled after the first row stops the read with
   `context.Canceled`.
6. A request whose connection closes after the server read it is sent once.
7. A request to a server that is not there wraps `driver.ErrBadConn`.
8. `BeginTx` returns `ErrNotSupported` if the driver has no transactions.

Each subtest calls `CheckGoroutines`, which fails if a goroutine that the
test started is still running when it ends. That catches a connector that
does not close its idle connections, and a body that nothing closed.
`CheckGoroutines` counts every goroutine of the process, so a test that
calls `RunContract` must not run in parallel with another test.

A driver sets `ContentType` when its bodies are not JSON, such as
`application/cbor`, and the fake servers send that content type.

The contract does not test paging (D21), because each product pages in its
own way. The driver tests that with its recorded exchanges.

### The survey

`testdata/<driver>/features.json` is the survey of step 5a, as
`dbimptest.Features`. It lists each model and each driver that the survey
asked, with the date, and one entry for each operation, feature and type.
An entry holds its kind (`crud`, `schema`, `feature` or `type`), its name,
the sources that named it, a verdict (`not measured`, `yes` or `no`), the
recorded file that shows what the server answered, and the test that
exercises it, as a function and a subtest. A type is named as the type
table names it. `TestEveryDriverHasItsFeatures` holds the rules for the
file, and the gates and `Replay` skip it.

### The round trip of a type

`RoundTrip` runs the round trip of one type for step 14a. The driver gives
it a `RoundTripCase`: the statements that set up and tear down the table,
insert a row with a key and a value, write the same row with literals,
select the value by the key, update it, and delete it, and at least two
values. For each value, once as a bound argument and once as a literal,
`RoundTrip` inserts the row, selects it and compares the value, updates it
to the next value, selects and compares again, deletes it, and selects to
see that it is gone.

It reads each value into a `*any` and compares with `reflect.DeepEqual`
unless the case supplies `Equal`, so the comparison covers the Go type. It
never selects a literal in place of a stored value. It tears the table down
in a cleanup, so the teardown runs when a test fails. For a database that
updates an index after a write, `Wait` makes it read again until the read
sees the write, with no fixed sleep. A value that has no literal in the
database is logged and skipped for the literal form only. For a database
that binds named parameters only, `Named` passes the key and the value as
`sql.Named("key", ...)` and `sql.Named("value", ...)`, and `KeyArg` turns
the key into the argument that the statements take, such as a record id.
It sends the teardown with a context that does not end with the test,
because the context of a test ends before its cleanup runs. Its tests hold a
store in memory with four faults: a value that changes its type, a delete
that keeps the row, a write that is late, and an update to NULL that keeps
the old value.

Three fields serve a database that cannot do every step, such as InfluxDB
(D86). `Column` names the column of the value, when the select returns other
columns with it. `SkipUpdate` returns why an update from one value to the
next cannot run, and `RoundTrip` logs the reason and skips that update and
the read after it. An empty `Delete` says that the database cannot delete one
row, and `RoundTrip` logs that and skips the delete and the check that the
row is gone. Each of the three keeps the old behaviour when it is not set.

### The two tables

`TypeTable` compares the type table of step 10 between the markers
`<!-- dbimp:types -->` and `<!-- /dbimp:types -->` in `docs/<PRODUCT>.md`.
`InterfaceTable` compares the interface table between
`<!-- dbimp:interfaces -->` and `<!-- /dbimp:interfaces -->`. It learns
whether each interface is implemented from the types of the driver, and it
fails if a reason is missing. Each one fails when the document holds another
table. With `DBIMP_UPDATE=1`, each writes its table into the document.

Each row of the type table has the kind of its type, from the list of
[TYPES.md](TYPES.md) (D137). `Kinds` sets it from a map of the wire types of
the driver, and fails for a type with no kind. `WriteBlock` compares or
writes any such block, and the root test `TestTheTypeMatrixIsCurrent` uses
it to write the table of every driver into [TYPES.md](TYPES.md).

## The points of W4

Each point of W4 is in the code or in a rule:

1. `driver.RowsColumnScanner`: `Assign` serves `ScanColumn`, and the fake
   driver in `dbimptest/fake_test.go` implements it.
2. One Go type for each wire type: the values section above, and
   `TypeTable`.
3. The raw JSON of each column, decoded when scanned: `ObjectRows` and
   `ArrayRows` keep a `jsontext.Value` for each column.
4. The database type name in upper case: `TypeTable` fails otherwise.
5. A NULL apart from an empty value: `IsNull`, `Assign`, and subtest 2 of the
   contract.
6. The connector owns its transport and closes it: `NewTransport`, and
   `CheckGoroutines` in each subtest of the contract.
7. `driver.NamedValueChecker` and `driver.ErrSkip`: a rule for each driver,
   in step 12 of [DRIVER.md](DRIVER.md).
8. Options from the DSN, then the context, then an argument: `Query` reads
   the DSN, and `Resolve` applies the context and the arguments (D109).
   `MarshalParams` writes the keys of `WithParameter`.
9. The error of the server before any rows: `CheckStatus`, and a rule for
   each driver, in step 12 of [DRIVER.md](DRIVER.md). `Send` never sends a
   request twice.
10. The DSN as a URL: `ParseURL` and `NewQuery`.
11. Fake servers from recorded responses: `Replay`.
12. A recording mode: `Recorder`.
13. The shared HTTP layer: `NewTransport`, `NewClient`, `Send` and `Stream`.
