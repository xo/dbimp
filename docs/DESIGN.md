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
| `url.go` | `ParseURL` and `Query`, for the DSN |
| `http.go` | `NewTransport`, `NewClient` and `Send` |
| `stream.go` | `Stream`, which reads the body of a response |
| `rows.go` | `ObjectRows` and `ArrayRows`, which read rows by D18 |
| `values.go` | The functions that turn a JSON value into a Go value, and `Assign` |
| `placeholder.go` | `Syntax`, which finds placeholders and binds arguments (D34) |

### Errors

A sentinel error is a constant of the type `Error` (D6). A driver wraps one
with `%w`, and a caller tests it with `errors.Is`:

- `ErrNotSupported` for a feature that the database does not have, such as a
  transaction (D20).
- `ErrScheme`, `ErrUnknownKey`, `ErrRepeatedKey` and `ErrInvalidValue` for a
  DSN (D27 and D35).
- `ErrExtraColumn` and `ErrColumnCount` for a row (D18).
- `ErrIncomplete` for a result that the server cut short (D21).
- `ErrUnterminated` and `ErrArguments` for placeholders (D34).

`CheckStatus` turns a response whose status is not 2xx into a `StatusError`.
It keeps the code and at most 64 KiB of the body, so that a driver can read
the error that its product writes there. HTTP 429 and HTTP 503 are a
`StatusError` like any other, and nothing retries them.

### The DSN

`ParseURL(name, dsn)` parses the DSN with `net/url`, and refuses a scheme
that is not the one name of the driver (D27 and D35). Its error never holds
the DSN, because the DSN can hold a password.

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
returns that object first. A key that a later object lacks is a nil value,
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
number through float64 unless the driver asks for a float64 (D19):

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

### Placeholders

`Syntax` says how a product writes literals, quoted identifiers and
comments. `Placeholders` finds each `?` and each `@name` outside them. An
`@` that another `@` follows, as in `@@version`, is not a placeholder. A
statement that ends inside a literal or a comment is `ErrUnterminated`.

`Bind` writes each argument into the statement with a function that the
driver supplies for the literals of its product (D34). A `?` takes the next
argument that has no name, and an `@name` takes the argument with that
name. A missing argument and an argument left over are both
`ErrArguments`. A fuzz test holds the parser.

## The package dbimptest

Only a test imports `dbimptest`. It imports `testing` and
`net/http/httptest`. Every driver imports the root package, so if the root
package held these helpers, every consumer of a driver links both packages
for code that only a test uses. So they are not in the root package (D37).

| File | Holds |
| --- | --- |
| `exchange.go` | `Exchange`, `ReadExchange`, `Match`, `DefaultMatch` and `Replay` |
| `manifest.go` | `Manifest`, `Entry`, `ReadManifest` and `WriteManifest` |
| `record.go` | `Recorder`, which writes the exchanges of step 6 |
| `goroutines.go` | `CheckGoroutines` |
| `contract.go` | `Contract` and `RunContract` |
| `tables.go` | `TypeTable` and `InterfaceTable` |

### Recorded exchanges

Each file under `testdata/<driver>/`, except the manifest, is one
`Exchange`: the method, the path, the query, the headers and the body of a
request, and the status, the headers and the body of its response.

`Recorder` is an `http.RoundTripper` that writes each exchange with a real
server. A driver test sets it as the transport of the client when
`DBIMP_RECORD` is set, and calls `Label` before each item of step 6, with
the number of the item and the principal. The recorder writes the value of
`Authorization`, `Cookie` and `Set-Cookie` as `REDACTED`. `WriteManifest`
adds what it recorded to `manifest.json`.

`Replay` starts a fake server that answers each request with the response of
the first exchange that matches it. `DefaultMatch` compares the method, the
path, the query, and the body, where two JSON bodies match if their
canonical forms are equal. A driver whose requests hold a value that changes
each time, such as an id for the request, supplies its own `Match`. A
request that nothing matches fails the test.

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
and the bodies of four results in the form of its product. Each subtest
starts a fake server:

1. The columns keep the order of the statement.
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

The contract does not test paging (D21), because each product pages in its
own way. The driver tests that with its recorded exchanges.

### The two tables

`TypeTable` writes the type table of step 10 between the markers
`<!-- dbimp:types -->` and `<!-- /dbimp:types -->` in `docs/<PRODUCT>.md`.
`InterfaceTable` writes the interface table between
`<!-- dbimp:interfaces -->` and `<!-- /dbimp:interfaces -->`. It learns
whether each interface is implemented from the types of the driver, and it
fails if a reason is missing. Each one fails when the document holds another
table. With `DBIMP_UPDATE=1`, each writes its table into the document.

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
   the DSN. The rest is a rule for each driver.
9. The error of the server before any rows: `CheckStatus`, and subtest 3 of
   the contract. `Send` never sends a request twice.
10. The DSN as a URL: `ParseURL` and `NewQuery`.
11. Fake servers from recorded responses: `Replay`.
12. A recording mode: `Recorder`.
13. The shared HTTP layer: `NewTransport`, `NewClient`, `Send` and `Stream`.
