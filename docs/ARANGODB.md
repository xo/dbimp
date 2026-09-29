# ArangoDB

This file holds what is known about the HTTP interface of ArangoDB, for the
driver that D73 places after InfluxDB, now that D88 removed CrateDB from
before it. W13 in [BACKLOG.md](BACKLOG.md) is the work. The headings are
the template of [DRIVER.md](DRIVER.md).

Step 6 measured the server on 2026-09-29, on `arangodb-3.12.12`, the one
release that `dbrun` starts, as the administrator `root` and as the ordinary
user `dbmeta_user`. A fact marked "measured" names its recorded file under
`testdata/arangodb/` by its number, so "031" is
`arangodb-3.12.12-031-post---db-dbmeta--api-cursor-...json`. A fact that no
server showed is "not measured", and names its source. The sources of step 3,
each read on 2026-09-28, are these:

- "The HTTP manual" is `site/content/arangodb/3.12/develop/http-api/` in
  `github.com/arangodb/docs-hugo`, at commit `6dece47`. The other documents
  of the vendor are in the same repository at the same commit:
  - "The AQL manual" is `site/content/arangodb/3.12/aql/`.
  - "The transactions manual" is
    `site/content/arangodb/3.12/develop/transactions/`.
  - "The release notes" are `site/content/arangodb/3.12/release-notes/`.
  - "The options" are `site/data/3.12/arangod.json`, which holds the
    default of each option of the server.
- "`errors.dat`" is `lib/Basics/errors.dat` in `github.com/arangodb/arangodb`
  at the tag `v3.12.12`. `LICENSE` is the file in the same repository.
- "The Go driver" is `github.com/arangodb/go-driver/v2` v2.4.1, the driver of
  the vendor. It is not in the module cache, so its code was read from
  GitHub. `usql` does not use it.
- "The dbmeta entry" is `container/arangodb.go` in `dbmeta`, from dbmeta
  commit 29395a1.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.1-pro-preview`, because `gemini-3.8-flash` timed out
  or closed the connection each time that it was asked. "DeepSeek" is
  `deepseek-flash`.

## Summary

- The product is ArangoDB, a database for documents and graphs, with the
  query language AQL (the AQL manual). The release is 3.12.12, the only one
  that the official image builds (the dbmeta entry).
- The licence: the source is under the Business Source License 1.1, and the
  official image under the ArangoDB Community License, which dbmeta D90
  counts as free for testing (the release notes and the dbmeta entry).
- R: `dbrun` starts it (measured). H: every call is HTTP with JSON
  (measured). S: AQL, by D75. The priority is P1.
- The driver is `github.com/xo/dbimp/arangodb`, which registers `arangodb`
  (D93). `dburl` has the provisional scheme `arangodb`, with the alias
  `arango` (dburl D32), and the alias `ar` that `dburl` adds itself for a
  scheme that lists no alias of two letters (dburl `v0.36.0`). Since dburl D34, its generator adds no port, and
  the driver uses 8529. `usql` has no driver for ArangoDB.
- `dbmeta` has no model for ArangoDB, and its entry is in the tier Staged
  (dbmeta D119), with the cadence tested (dbmeta D120).
- D89 to D94 settle step 9, with D99. D103 and D104, which cover items 4, 6,
  and 10 to 12, wait for Ken.

## Requests

- A query is `POST /_db/<database>/_api/cursor`, with a JSON body of `query`,
  `bindVars`, `batchSize`, `count`, `ttl`, `memoryLimit` and `options`
  (measured, 009). A body sent as text works too (measured, 010).
- The driver sends `options.stream: true` and the `batchSize` of the DSN
  (D90). The next batch is `POST /_api/cursor/<id>` (measured, 029).
- HTTP Basic works for both principals (measured, 007 and 126). A wrong
  password is HTTP 401 with `errorNum` 11 (measured, 058). An access token
  can stand in for the password, and a JWT goes as a Bearer token (the HTTP
  manual, not measured). D94 names the key `auth=basic|bearer`.
- The ordinary user has no access to `_system`: `/_api/version` there is HTTP
  401 (measured, 127).
- The default port is 8529 (the HTTP manual and the dbmeta entry).
- AQL has no DDL: `CREATE COLLECTION` is a parse error (measured, 090). Every
  collection, index, view, graph, analyzer and database is an endpoint
  (measured, 092 to 111). The driver takes DDL of its own for collections and
  indexes (D92).
- A geo index on one field with `geoJson: false`, the default of the
  server, reads an array as `[latitude, longitude]`, and does not index a
  GeoJSON object. With `geoJson: true`, it reads an array as
  `[longitude, latitude]`, and indexes a GeoJSON object (measured with
  `curl` on 2026-09-29). D106 sends `true`.

## The DSN

D93 and D94 settle it: `arangodb://user:password@host:port/database`. The
path is the database, and `_system` without one. The port is 8529 without
one. The keys are these, and any other key is refused:

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `cancel` | `tag`, `none` (D90) | `tag` |
| `batch` | the `batchSize` of each cursor, 1 or more | `1000` |
| `auth` | `basic`, `bearer` (D94) | `basic` |

The dbmeta entry prints the `url`
`arangodb://root:<password>@127.0.0.1:<port>/dbmeta` for the administrator
and the same for `dbmeta_user`, from dbmeta commit 29395a1.

## Responses

- A batch is one object: `result`, then `hasMore`, `id`, `extra`, `cached`,
  `nextBatchId`, `error` and `code` (measured, 028). `result` always comes
  first (measured in every recording), so the rows stream before the id of
  the cursor, which follows them.
- A new cursor answers HTTP 201, and a next batch HTTP 200 (measured, 028 and
  029). Each batch has a `Content-Length` (measured), so the server builds one
  batch at a time.
- The response holds no list of columns. Each value of `result` is what
  `RETURN` gave (measured, 015). D89 makes columns of it.
- An object keeps the keys in the order of the query:
  `RETURN {b: 1, a: 2, c: null}` gives `b`, `a`, `c` (measured, 011).
- A stored document keeps the order of its insert, after `_key`, `_id` and
  `_rev` (measured, 012). A stored edge starts with `_key`, `_id`, `_from`,
  `_to` and `_rev` (measured with `curl` on 2026-09-29). Documents of one
  collection can have different attributes (measured, 013).
- A projection reads a missing attribute as `null` (measured, 014).
- The default `batchSize` is 1000, and a larger one than the result gives one
  batch (measured, 035 and 057). A batch after the last is HTTP 404 with
  `errorNum` 1600 (measured, 031).
- Without `stream`, the server computes the whole result first, and an error
  arrives before any row (measured, 042). With `stream`, it computes each
  batch when the client asks, and `extra` comes only with the last batch
  (measured, 032 to 034).
- `count` gives the count of the whole result, without `stream` (measured,
  038). `fullCount` does not work with `stream` (the HTTP manual).
- `memoryLimit` stops a query that uses more memory, with HTTP 500 and
  `errorNum` 32 (measured, 056).
- The server does not compress, even when the client asks with
  `Accept-Encoding: gzip` (measured, 082).
- No recorded answer is a redirect.

## Types

The type table is written from the code by `TestTables` (step 10).

<!-- dbimp:types -->
| Wire type | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- |
| null | `nil` | `interface {}` | `` | yes |
| boolean | `bool` | `interface {}` | `` | yes |
| integer | `int64, for a number with no fraction that fits` | `interface {}` | `` | yes |
| double | `float64, and a number above the range of int64` | `interface {}` | `` | yes |
| string | `string` | `interface {}` | `` | yes |
| array | `[]any` | `interface {}` | `` | yes |
| object | `map[string]any, and a document in one column (D89)` | `interface {}` | `` | yes |
<!-- /dbimp:types -->

- A number with no fraction that fits `int64` arrives as that integer, such
  as `9223372036854775807` (measured, 019). A number above that range is a
  double in AQL: `18446744073709551615` arrives as `18446744073709552000`, and
  the literal `-9223372036854775808` as `-9223372036854775808.0` (measured,
  019). So the driver reads a number with no fraction as an `int64` when it
  fits, and every other number as a `float64`.
- A number bound as a parameter keeps its value: `9223372036854775807`
  (measured, 027).
- A division by zero is `null`, with the warning 1562 (measured, 020).
- AQL has no date type: `DATE_ISO8601(0)` is the string
  `1970-01-01T00:00:00.000Z`, and `DATE_NOW()` a number of milliseconds
  (measured, 019). No binary, decimal or UUID type exists (the AQL manual).
  A binary argument is refused by the driver.

## Parameters

- The server binds each parameter from `bindVars` (measured, 022). `@1` is a
  name (measured, 023), so a positional argument n fills `@n`.
- `@@name` names a collection, and its key in `bindVars` is `@name`
  (measured, 024). The driver sends a named argument that the query uses as
  `@@name` with that key.
- A missing parameter is `errorNum` 1551, and an extra one 1552 (measured,
  025 and 026).

## Transactions

- A stream transaction begins with `POST /_api/transaction/begin`, which names
  its write collections, and each statement in it sends
  `x-arango-trx-id` (measured, 068 and 069).
- A write in the transaction is not seen outside it, and an abort removes it
  (measured, 070 to 072). A commit keeps it (measured, 073 to 076).
- A write to a collection that the transaction did not name fails with
  `errorNum` 1652, and a read works (measured, 079 and 080).
- The ordinary user can run a transaction on its collections (measured, 184
  to 197). D91 says how `BeginTx` works.

## Errors

- An error is `{"code", "error": true, "errorMessage", "errorNum"}`, with a
  status that is not 2xx (measured, 040). No recorded error came with HTTP
  200.
- A parse error is HTTP 400 and 1501, a collection that does not exist HTTP
  404 and 1203, and a unique key twice HTTP 409 and 1210 (measured, 040, 041
  and 102).
- An error after some rows of a stream is the answer to the next fetch, with
  HTTP 500 (measured, 043 and 044). The cursor is then gone (measured, 045).
- A schema that refuses a document is HTTP 400 and 1620 (measured, 107).
- No recorded answer is HTTP 429, and none is HTTP 503.

## Cancellation and timeouts

- A closed connection does not stop a query: the query goes on to its end
  (measured with `curl` on 2026-09-29).
- `/_api/query/current` lists the running queries with their text, and
  `DELETE /_api/query/<id>` kills one (measured, 047 and 048). The ordinary
  user can do both for its own queries (measured, 163 and 164).
- A comment in the text stays in that list (measured, 054, with the comment
  at the start).
- The list keeps the first 4096 bytes of the text, which is
  `maxQueryStringLength` of `/_api/query/properties`, and puts
  `... (<n>)` in place of the rest. So a query of 6000 bytes lost the
  comment at its end (measured with `curl` on 2026-09-29). Both principals
  can read `/_api/query/properties` (measured with `curl` on 2026-09-29).
  The comment of `cancel=tag` at the end of a longer query is lost the same
  way, so D99 puts it at the start of such a query.
- `DELETE` on a streaming cursor stops its query (measured, 050 to 052).
- A killed streaming query keeps its cursor, and its collections, until the
  next fetch or the end of its `ttl` (measured with `curl` on 2026-09-29).
  The next fetch answers HTTP 410 with 1500, and a drop of the collection
  then runs at once (measured with `curl` on 2026-09-29).
- `maxRuntime` kills a query after that time, with HTTP 410 and 1500
  (measured, 053). The driver does not send it.
- D90 says how the driver stops a query.

## Statements

- A query holds one statement: `RETURN 1; RETURN 2` is a parse error
  (measured, 066).
- `//` and `/* */` comments work (measured, 067).
- A query cannot write to a collection after it read it, or write to it
  twice (measured, error 1579 in the first recording of step 6).

## Principals

- `root` is the administrator. `dbmeta_user` has `rw` on `dbmeta` and no
  access to `_system` (the dbmeta entry, measured, 127).
- The ordinary user can read, write, make and drop collections, run
  transactions, and list and kill its own queries (measured, 179, 180, 184
  and 163). It cannot list the collections of `_system` or make a database
  (measured, 176 and 177).
- The version is `/_db/<database>/_api/version`, which both principals can
  read (measured, 007 and 126), and `VERSION()` in AQL. `RETURN VERSION()`,
  the statement that step 16 proposes to `usql`, gives `3.12.12` to the
  administrator (measured, 065) and to the ordinary user (measured, 181).

## Flavors

- No other product speaks this API (Gemini). The vector index needs the
  server option `--vector-index`, which the dbmeta entry does not set
  (measured, 101).

## Interfaces

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping reads the version of the database, which checks the credentials and the database. |
| `driver.SessionResetter` | no | A transaction ends before database/sql hands the connection on, so nothing needs a reset. |
| `driver.Validator` | no | A connection holds no state on the server outside a transaction, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps a uint64, a decimal, a slice and a map, which AQL takes as they are. |
| `driver.QueryerContext` | yes | The server binds each argument itself, through bindVars. |
| `driver.ExecerContext` | yes | Exec reads the result to its end, and RowsAffected is writesExecuted. |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | A stream transaction that names every collection of the database (D91). |
| `driver.RowsColumnScanner` | yes | A value is decoded from JSON when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A query holds one statement, so a cursor has one result. |
| `driver.RowsColumnTypeScanType` | no | The cursor names no types, and each value names its own. |
| `driver.RowsColumnTypeDatabaseTypeName` | no | The cursor names no types. |
| `driver.RowsColumnTypeLength` | no | The cursor names no types, so no column has a length. |
| `driver.RowsColumnTypeNullable` | no | The cursor names no types, and any value can be null. |
| `driver.RowsColumnTypePrecisionScale` | no | The cursor names no types, so no column has a precision or a scale. |
<!-- /dbimp:interfaces -->

## Faults

`usql` has no ArangoDB driver, so there is no fault of `usql` to record.
These are faults of the Go driver of the vendor, which this driver does not
repeat (the Go driver):

- `Close` on a cursor deletes it with `context.Background`.
- It decodes each batch whole, with `json.Unmarshal`, where D25 asks for one
  token at a time.
- Nothing kills the query when the context ends.

## Second opinions

Step 7 settles each lead against the server. Each model was asked on
2026-09-28.

- The order of the keys of an object. Gemini said that the server sorts
  the keys, because VelocyPack sorts them, and that `_key`, `_id` and `_rev`
  come first in a document. DeepSeek said that the server keeps the order of
  the query or of the insert, and does not guarantee it. The example of
  `RETURN u` in the AQL manual shows the keys in alphabetical order. The
  first example of the HTTP manual shows `name` before `_rev`, `_key` and
  `_id`. So the sources disagree, and step 6 measures it. D18 rule 2 depends
  on it.
- The order of the fields of the body. DeepSeek said that `result` comes
  before `hasMore`, `id` and `extra` in practice. The examples of the HTTP
  manual show both orders.
- The query id. Both models said that the cursor response holds a cursor id
  and not a query id. DeepSeek said that an ordinary user can list and kill
  its own queries. Gemini was unsure. The Go driver answers HTTP 403 on
  `/_api/query/` with a message that the user needs admin rights.
- A closed connection. Both models said that it does not stop a running
  query, as the HTTP manual says.
- `DELETE` on a streaming cursor. Both models said that it stops the query.
- `@1` as a parameter. DeepSeek said that a name must start with a letter
  or an underscore. Gemini was unsure. The AQL manual says that a name can
  start with a digit.
- A write to an undeclared collection in a stream transaction. Both models
  said that it fails, and DeepSeek named error 1652.
- Isolation of a stream transaction on a single server. Gemini said snapshot
  isolation. DeepSeek said read committed, with an option
  `isolationLevel` for snapshot. No source read here names that option.
- Large integers. DeepSeek said that an integer above 2^53 arrives as a
  string, from an option `--json.return-large-numbers-as-strings`. Gemini
  said that each one arrives as a JSON number. The release notes describe
  numbers with no such option.
- Native types. Both models said that decimal and UUID are not native.
  Gemini said that VelocyPack has a date and a binary type. DeepSeek said
  that there is no date and no binary type.
- The version. Both models said that `/_api/version` fails for a user with
  no access to `_system`, and that `/_db/<database>/_api/version` works.
- Compression. Both models said that the server compresses when the client
  asks. Gemini named a threshold of 1024 bytes by default. The release notes
  and the options give a threshold of 0 by default, which turns compression
  off.

Step 7 asked Gemini on 2026-09-29, and DeepSeek gave no answer:

- A comment in the text finds a query in `/_api/query/current`. True
  (measured, 054).
- `memoryLimit` caps a query. True (measured, 056).
- No option returns a large integer as a string. Not measured, and the
  release notes name none.
- A batch is built whole in memory, not streamed inside the response. True:
  each batch has a `Content-Length` (measured).

These leads of step 3 were then measured:

- The order of the keys of an object: the order of the query (measured,
  011). Gemini said sorted, which is false for 3.12.12.
- `@1` as a parameter works (measured, 023).
- A write to an undeclared collection in a stream transaction fails with 1652
  (measured, 079), as both models said.
- An integer above 2^53 arrives as a JSON number (measured, 019), as Gemini
  said.
- Compression is off by default (measured, 082), as the options say.

## Open questions

Ken decided step 9 on 2026-09-29, in D89 to D94, and D99. The review of
D97 found these, and each one waits for Ken:

- D103 and D104, which propose the choices of the code for items 4, 6, and
  10 to 12 of step 9.

- The questions under "Compared with Couchbase" below.

## The test runs

The integration tests passed on `arangodb-3.12.12` on 2026-09-29, as both
principals, including the round trip of every type (step 14a).

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-29 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of ArangoDB from the sections above.

### The server

| | Couchbase | ArangoDB |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /_db/<database>/_api/cursor`, with `query` and `bindVars` |
| Database | The key `query_context` of the body | The path, `/_db/<database>/` |
| Language | SQL++, which is close to SQL | AQL, which is not SQL: `FOR d IN c RETURN d` |
| DDL | In SQL++ | None in AQL. Each collection and index is an endpoint (measured, 090 to 111) |
| Parameters | `?`, `$1` and `$name` | `@name`, `@1` as a name, and `@@name` for a collection |
| Framing | One body for the whole result, which does not page | A cursor that sends batches, with one request for each next batch |
| Columns | `signature`, before the first row | No list. Each value is what `RETURN` gave |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The query, and a stored document in the order of its insert |
| Errors | Can come with HTTP 200, after some rows | Never with HTTP 200. An error of a stream is the answer to the next fetch, with HTTP 500 |
| Types | JSON. No date, decimal, UUID or binary | JSON. No date, decimal, UUID or binary |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves. The driver kills it by its tag, or deletes its cursor (D90) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `POST /_api/transaction/begin`, which names its collections, carried by `x-arango-trx-id` |
| Authentication | Basic, or `creds` in the body | Basic, or a Bearer token |
| Default port | 8093, or 18093 with TLS | 8529 |

The differences that a caller sees:

- A result has no list of columns, so the driver makes them from the shape of
  the rows (D89). A projection gives its keys as columns, and a document is
  one column.
- A write needs a transaction that names the collection before it begins, so
  `BeginTx` names every collection of the database (D91).
- DDL is not AQL, so the driver takes DDL of its own (D92).
- The server does not stop a query when the client leaves, so the driver
  kills it by its tag (D90).

### The driver

| | `couchbase` | `arangodb` |
| --- | --- | --- |
| Size, without tests, on 2026-09-29 | About 1300 lines in 8 files | About 1800 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Database`, `Cancel`, `Batch`, `Auth` |
| Options for one statement | Five `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40 and D46). `WithParameter` sets any key of the body | None |
| Arguments | Sent to the server as `args` and `$name` | Sent in `bindVars`. `@@name` is sent under the key `@name` (`values.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A cursor of its own (`rows.go`). It fetches each batch with the context of `QueryContext`. `Close` deletes the cursor, or in the first batch kills the query by its tag when more than 1 MiB of the batch is left (D90) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | None |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | `int64` for a number that fits, and `float64` for any other. A document is a map in one column. A binary argument is refused |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `writesExecuted`. `LastInsertId` returns `dbimp.ErrNotSupported` |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` begins a stream transaction. `ReadOnly` names no collection for write |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | `cancel=tag` kills the query by its tag, or `cancel=none` (D90) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Num, Message}`, which unwraps to `*dbimp.StatusError` |
| Authentication | Basic | `auth=basic` or `auth=bearer` (D94) |
| Other exports | The `With` options and `Option` | `CancelTag`, `CancelNone`, `AuthBasic` and `AuthBearer` |

The differences that a caller sees:

- The columns have no database type, and the scan type of each is `any`,
  because the cursor sends no list of columns and no types (measured, 015,
  and D103, proposed).
- The DSN names the database in its path, and takes the keys `tls`,
  `cancel`, `batch` and `auth`, where Couchbase takes others (D93 and D94).
- A caller of ArangoDB has no option for one statement. A timeout for one
  query is the context of the call. No decision says why. This is a question
  for Ken.
- The driver has no `ResetSession`. A transaction comes only from `BeginTx`,
  because AQL has no statement that begins one, and `database/sql` always
  ends a transaction before it reuses the connection (D102).
- An integer too large for `int64` reads as a `float64`, where Couchbase
  gives an `*apd.Decimal`. AQL itself holds such a number as a double, so
  the digits are gone before the driver reads them (measured, 019, and D103,
  proposed).
- A `[]byte` argument is refused, where Couchbase sends it as base64 (D44).
  AQL has no binary type, and D104, proposed, says why the driver invents
  no convention.
- An error is one value with `errorNum`, where Couchbase holds a list. The
  server sends one error for each response (measured, 040, and D103,
  proposed).
