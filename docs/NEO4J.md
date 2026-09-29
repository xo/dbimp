# Neo4j

This file holds what is known about the HTTP interface of Neo4j, for the
third driver, `github.com/xo/dbimp/neo4j`. W9 in [BACKLOG.md](BACKLOG.md) is
the work, and it follows [DRIVER.md](DRIVER.md). The headings are the
template of that file.

Each fact says how it was measured. "Recorded" means that an exchange under
`testdata/neo4j/` holds it, for 5.26.31 and 2026.09.0, as the administrator
and as the ordinary user, recorded on 2026-09-27 from the script
`testdata/neo4j/requests.json` with `dbimptest/cmd/record`. A fact that only
one release or one principal shows names it. "Measured by hand" means that
this session sent the request from a script on 5.26.31 and 2026.09.0 on
2026-09-27, or on the date that the fact names, and no file holds it. "The manual" is the Query API manual of
Neo4j at `neo4j.com/docs/query-api/current/`, read on 2026-09-27. "The Go
driver" is `github.com/neo4j/neo4j-go-driver/v6` v6.3.0, and "the Python
driver" is the PyPI package `neo4j` 6.3.1, both read on 2026-09-27. Both
speak Bolt, not HTTP. "The models" are Gemini and DeepSeek, asked on
2026-09-27. "Tested" means that an integration test in
`neo4j/integration_test.go` holds it, and passed on 5.26.31 and 2026.09.0
as both principals on 2026-09-27. A fact marked "not measured" is a lead,
not a fact.

## Summary

- The product is Neo4j, a graph database, with the query language Cypher.
  Cypher meets S by D58, so it is P1.
- `dbrun` runs the Enterprise Edition under the evaluation agreement of
  Neo4j, from dbmeta D106 and dbimp D59. It names two releases in the Tested
  tier: `neo4j-5.26.31`, the release of long term support, which is the
  floor, and `neo4j-2026.09.0`, the newest monthly release, which is the
  ceiling. Each is the image `docker.io/library/neo4j`. The entry is dbmeta
  commit `1335428` on `main` (read on 2026-09-27). dbmeta D109, in commit
  `19a8a9a`, makes it the dialect `neo4j`, and `dbrun` prints the URL of D61
  for each principal (read on 2026-09-28).
- The Community Edition has no roles, so it has no ordinary user. The image
  of 4.4 takes only a commercial licence (dbmeta D106).
- dburl D28 adds the scheme `neo4j`, with the aliases `nj`, `neo` and `n4j`,
  which Ken chose on 2026-09-27. dburl `v0.35.0` releases it. `usql` has no driver for Neo4j.
- The survey of step 5a is `testdata/neo4j/features.json`. Each entry is
  settled against 2026.09.0 and recorded on both releases, as both
  principals, and each one names the integration test that holds it (step
  14a).
- The integration tests passed on `neo4j-5.26.31` and `neo4j-2026.09.0`, as
  both principals, on 2026-09-27, and again on 2026-09-29 with D95. On 5.26.31, the entries that need a later
  release skip with the release that they need: Cypher 25, the clauses of
  GQL, a vector and a UUID.
- The driver is `github.com/xo/dbimp/neo4j`, from D60 to D69, D95, D100 and
  D105.

## Requests

- A statement is `POST /db/<database>/query/v2`, with the body
  `{"statement": ..., "parameters": {...}}` (recorded). The path names the
  database. `/db/neo4j/query/v2` reaches the default database `neo4j`, and a
  database that does not exist is HTTP 404 with
  `Neo.ClientError.Database.DatabaseNotFound` (recorded).
- `GET /` gives the discovery document, with `neo4j_version`,
  `neo4j_edition` and the template of each endpoint (recorded). 2026.09.0
  adds `query_api_versions: ["2.0"]`. Neither release names a media type in
  it (recorded).
- The older HTTP API, `POST /db/<database>/tx/commit`, still answers on both
  releases (recorded). The manual calls it deprecated.
- Authentication is basic. A wrong password is HTTP 401 with
  `Neo.ClientError.Security.Unauthorized` and the header
  `Www-Authenticate: Basic realm="Neo4j", Bearer realm="Neo4j"` (recorded).
  A Bearer token is HTTP 401 with "Unsupported authentication token:
  scheme='bearer'", on both releases, because Neo4j takes one only from an
  identity provider of single sign on, which the server of dbrun has not
  (measured by hand on 2026-09-29, and tested).
- `Accept: application/json` gives plain JSON.
  `Accept: application/vnd.neo4j.query` gives typed JSON, in which each value
  is an object with a `$type` and a `_value` (recorded).
- Each release takes some of the versions of the typed media type
  (recorded):

  | Accept | 5.26.31 | 2026.09.0 |
  | --- | --- | --- |
  | `application/vnd.neo4j.query` | 202, as v1.0 | 202, as v1.0 |
  | `application/vnd.neo4j.query.v1.0` | 202 | 202 |
  | `application/vnd.neo4j.query.v1.1` | 406 | 202 |
  | `application/vnd.neo4j.query.v1.2` | 406 | 202 |
  | `application/jsonl` | 406 | 202 |
  | `application/vnd.neo4j.query.v1.0+jsonl` | 406 | 202 |

- An `Accept` list with weights, such as
  `application/vnd.neo4j.query.v1.2, application/vnd.neo4j.query.v1.1;q=0.9, application/vnd.neo4j.query;q=0.8`,
  gets the best version that the server has. 5.26.31 answers
  `application/vnd.neo4j.query`, and 2026.09.0 answers
  `application/vnd.neo4j.query.v1.2` (recorded). The `Content-Type` of the
  response names the version.
- `Content-Type: application/vnd.neo4j.query` sends the parameters in typed
  JSON on both releases (recorded). `Content-Type:
  application/vnd.neo4j.query.v1.2` is HTTP 415 on 5.26.31 (recorded).
- A body that is not JSON, or one with no statement, is HTTP 400 with
  `Neo.ClientError.Request.Invalid` (recorded).
- A JSON string longer than 20,000,000 characters is HTTP 400 with
  `Neo.ClientError.Request.Invalid` and the message "Bad Request", on both
  releases. A body of 60 MB made of short strings is accepted (measured by
  hand).
- The HTTP interface is on port 7474, and HTTPS on 7473 (the manual). Bolt,
  on 7687, is not HTTP.

## The DSN

- D61 decides the URL `neo4j://user:pass@host:port/<database>`, such as
  `neo4j://neo4j:pw@127.0.0.1:7474/dbmeta`. With no path, the database is
  `neo4j`. A path of more than one segment is refused.
- The key `tls` is `false` by default, and `tls=true` makes the driver speak
  HTTPS. The default port is 7474, and 7473 with `tls=true`. D67 decides
  the key `cancel`, with `tag`, the default, `metadata` or `none`, and D95
  puts the tag at the end of each statement. Every other key, and a key
  given twice, is refused (D61).
- The driver sends the user and the password with basic authentication. A
  URL with no user and no password sends no authentication. With
  `auth=bearer`, it sends the password as a Bearer token, and no user
  (D110 and D116).
- Each key of the DSN that can change for one statement is also an option
  (D109). `WithDatabase` sets the database of the path, and `WithCancel`
  sets `cancel`. A statement of a transaction runs in the database of the
  transaction, and `WithDatabase` with another database fails with
  `dbimp.ErrNotSupported`.
- The tools of Neo4j write `neo4j://host:7687` for Bolt with routing. A URL
  copied from them names the port of Bolt, and the request fails (D60).

## Responses

- A response is one JSON object with `data`, which holds `fields`, the names
  of the columns, and then `values`, one array for each row (recorded). The
  columns arrive before the rows, and keep the order of the statement:
  `RETURN 1 AS b, 2 AS a, 3 AS c` gives `["b", "a", "c"]` (recorded).
- A statement that returns nothing, such as a `CREATE` with no `RETURN`,
  gives `fields: []` and `values: []` (recorded).
- After `data` come `bookmarks` on both releases, and `queryType`,
  `resultAvailableAfter` and `resultConsumedAfter` on 2026.09.0 (recorded).
  `notifications` holds each warning, such as
  `Neo.ClientNotification.Statement.UnknownLabelWarning` (recorded).
  `"includeCounters": true` in the request adds `counters`, such as
  `nodesCreated` and `containsUpdates` (recorded).
- A result of 20,000 rows arrives with no `Content-Length`, so the server
  sends it in chunks (recorded). A small result has a `Content-Length`.
- On 2026.09.0, a JSON Lines media type gives one event on each line: a
  `Header` with the fields, a `Record` for each row, then a `Summary` or an
  `Error` (recorded). 5.26.31 refuses it with HTTP 406.
- The server sends no `Content-Encoding` when the request asks for gzip, on
  either release (recorded). No response was a redirect or HTTP 429
  (recorded).
- The Query API has no cursor and no paging. A client pages with `SKIP` and
  `LIMIT` in the statement (the models, not measured).

## Types

`neo4j/tables_test.go` writes this table from the code (step 10). Every
column can be NULL, and no type arrives for a column, so each scans into
`any`.

<!-- dbimp:types -->
| Wire type | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- |
| null | `nil, from the $type Null` | `interface {}` | `` | yes |
| boolean | `bool, from the $type Boolean` | `interface {}` | `` | yes |
| integer | `int64, from the $type Integer` | `interface {}` | `` | yes |
| float | `float64, from the $type Float` | `interface {}` | `` | yes |
| string | `string, from the $type String` | `interface {}` | `` | yes |
| byte array | `[]byte, from the $type Base64` | `interface {}` | `` | yes |
| list | `[]any, from the $type List` | `interface {}` | `` | yes |
| map | `map[string]any, from the $type Map` | `interface {}` | `` | yes |
| date | `neo4j.Date, from the $type Date` | `interface {}` | `` | yes |
| local time | `neo4j.LocalTime, from the $type LocalTime` | `interface {}` | `` | yes |
| zoned time | `neo4j.Time, from the $type Time` | `interface {}` | `` | yes |
| local datetime | `neo4j.LocalDateTime, from the $type LocalDateTime` | `interface {}` | `` | yes |
| offset datetime | `time.Time, from the $type OffsetDateTime` | `interface {}` | `` | yes |
| zoned datetime | `time.Time, in the location that the zone names, from the $type ZonedDateTime` | `interface {}` | `` | yes |
| duration | `neo4j.Duration, from the $type Duration` | `interface {}` | `` | yes |
| point | `neo4j.Point, from the $type Point` | `interface {}` | `` | yes |
| node | `neo4j.Node, from the $type Node` | `interface {}` | `` | yes |
| relationship | `neo4j.Relationship, from the $type Relationship` | `interface {}` | `` | yes |
| path | `neo4j.Path, from the $type Path` | `interface {}` | `` | yes |
| vector | `neo4j.Vector, from the $type Vector` | `interface {}` | `` | yes |
| uuid | `uuid.UUID, from the $type UUID` | `interface {}` | `` | yes |
<!-- /dbimp:types -->

These are the forms that the server sends (recorded, unless the fact says
otherwise):

- An integer is 64 bits. Typed JSON writes it as a string, such as
  `"9223372036854775807"`, and plain JSON writes it as a JSON number, which
  keeps all 64 bits. A literal larger than the largest int64 is a syntax
  error.
- A float is a float64. Typed JSON writes it as a string, such as `"0.1"`,
  `"1.5E300"`, `"NaN"` or `"Infinity"`. Plain JSON writes NaN and infinity as
  the strings `"NaN"` and `"Infinity"`, so plain JSON cannot tell them from a
  string.
- Neo4j has no decimal type. `RETURN 1.23456789012345678901234567890` gives
  the Float `1.2345678901234567`.
- NULL is `{"$type": "Null", "_value": null}` in typed JSON and `null` in
  plain JSON. A property that a node does not have reads as NULL, so NULL and
  a missing value are one value.
- A byte array is `Base64`, such as `"3q2+7w=="`, in typed JSON.
  `valueType` names a byte array parameter `LIST<INTEGER NOT NULL> NOT NULL`
  (recorded on both releases).
- A list is `List`, and a map is `Map`. The server sorts the keys of a map:
  `{b: 1, a: 2, c: 3}` arrives as `{"a": 2, "b": 1, "c": 3}`. The properties
  of a node arrive in no sorted order.
- The temporal types are `Date`, `LocalTime`, `Time`, `LocalDateTime`,
  `OffsetDateTime` and `ZonedDateTime`, with nanoseconds, such as
  `"12:50:35.556123456"`. A `ZonedDateTime` is
  `"2026-09-27T10:00:00+02:00[Europe/Oslo]"`. Plain JSON drops the name of
  the zone, and writes each temporal value as a string, so plain JSON cannot
  tell a date from a string.
- `datetime('...')` refuses an offset with seconds, such as `+01:30:15`,
  with "Text cannot be parsed to a DateTime". The form of a map,
  `datetime({epochSeconds: ..., timezone: '+01:30:15'})`, and a typed
  argument take it (tested).
- A vector and a UUID can be a property on 2026.09.0 (tested).
- A date runs from `-999999999-01-01` to `+999999999-12-31`. A `time.Time`
  of Go holds both ends, but `time.Parse` and `Time.MarshalText` take a year
  of four digits only (measured with Go 1.27.1 on 2026-09-27).
  `datetime('1600-01-01T00:00:00Z')` is an `OffsetDateTime`.
- A duration is `Duration`, such as `"P1Y2M3DT4H5M6.007S"`, with months and
  days apart from the seconds. A `time.Duration` holds no months.
- A point is `Point`, such as `"SRID=7203;POINT (1.5 2.5)"` or
  `"SRID=4979;POINT Z (10.7 59.9 3.0)"`, in both forms of JSON.
- A node is `Node`, with `_element_id`, `_labels` and `_properties`. A
  relationship is `Relationship`, with `_element_id`,
  `_start_node_element_id`, `_end_node_element_id`, `_type` and
  `_properties`. A path is `Path`, a list of nodes and relationships in
  turn. Plain JSON uses `elementId`, `labels`, `properties`,
  `startNodeElementId`, `endNodeElementId` and `type`, and writes a path as a
  plain array.
- A vector is `Vector`, with `coordinatesType` and `coordinates`, such as
  `{"coordinatesType": "FLOAT32", "coordinates": ["1.0", "2.0", "3.0"]}`, in
  typed JSON v1.1 on 2026.09.0. Typed JSON v1.0 on 2026.09.0 sends the row as
  `[]` and then the error "Type VECTOR is not supported.", so a row can hold
  fewer values than `fields`. Plain JSON writes a vector as a list of
  numbers. 5.26.31 has no vector function.
- A UUID is `UUID` in typed JSON v1.2 on 2026.09.0, sent as a parameter.
  `randomUUID()` gives a `String` on both releases. 5.26.31 has no UUID type.
- A list of mixed types, or a map, cannot be a property. The error arrives
  after the rows, with `Neo.ClientError.Statement.TypeError`.

## Parameters

- Each parameter is named, as `$name`, and goes in `parameters` (recorded).
  The name `$0` works, with the key `"0"` (recorded), and so do `$1` and
  `$2` (recorded).
- `database/sql` gives each argument the ordinal of its place among every
  argument, named or not. So a positional argument after
  `sql.Named("a", ...)` has the ordinal 2, and fills `$2` (tested).
- A statement that names a parameter that the request does not give is HTTP
  400 with `Neo.ClientError.Statement.ParameterMissing`. A parameter that no
  statement names is ignored (recorded).
- In plain JSON, `1` is an `INTEGER`, and `1.0` and `1e3` are each a
  `FLOAT`. `9007199254740993` and `9223372036854775807` arrive exact
  (recorded). `9223372036854775808` is HTTP 400: 5.26.31 gives the code
  `N/A` and "Unable to convert java.math.BigInteger to Neo4j Value.", and
  2026.09.0 gives `Neo.ClientError.Request.Invalid` (recorded).
- In typed JSON, a parameter of type `Date`, `Duration`, `Point`,
  `ZonedDateTime`, `Integer`, `Float`, `String` or `Base64` keeps its type
  (recorded). A `Node` parameter is HTTP 400 on both releases (recorded).

## Transactions

- `POST /db/<database>/query/v2/tx` opens an explicit transaction, with or
  without a statement. The response gives `transaction.id` and
  `transaction.expires` (recorded). A statement goes to
  `/query/v2/tx/<id>`, `POST /query/v2/tx/<id>/commit` commits, with or
  without a last statement, and `DELETE /query/v2/tx/<id>` rolls back
  (recorded).
- A write in the transaction is not seen outside it before the commit, and
  is seen after it. A rollback leaves nothing (recorded).
- The rollback is HTTP 200, with an empty body on 5.26.31 and `{}` on
  2026.09.0 (recorded).
- An error in a statement ends the transaction. The error arrives with HTTP
  202, and the next request to the transaction is HTTP 404 with "Transaction
  with Id ... was not found" (recorded). A transaction that does not exist
  is the same 404.
- `CALL {} IN TRANSACTIONS` in an explicit transaction is refused with
  `Neo.DatabaseError.Transaction.TransactionStartFailed`, as HTTP 500 on
  5.26.31 and HTTP 400 on 2026.09.0, and the transaction is gone. It works in
  an implicit query (recorded).
- `"accessMode": "READ"` in a request, or in the request that opens a
  transaction, refuses a write with HTTP 400 and
  `Neo.ClientError.Statement.AccessMode`, on both releases (recorded). An
  access mode that does not exist is HTTP 400.
- `expires` is 60 seconds after the `Date` of the response that opened the
  transaction (recorded). The manual names
  the setting `server.queryapi.transaction_idle_timeout`.
- Until then, the server holds the locks of the writes of a transaction
  that nobody rolled back, and a write of the same node from outside waits
  (tested, by `TestIntegrationRollbackAfterTheContext`, on 2026-09-29). So
  `Rollback` sends its request even after the context of `BeginTx` ends
  (D100).
- On a cluster, a transaction needs the header `neo4j-cluster-affinity`
  (the manual, not measured). `dbrun` runs one server.

## Errors

- An error is `{"errors": [{"code": ..., "message": ...}]}`, with a code
  such as `Neo.ClientError.Statement.SyntaxError` (recorded).
- A syntax error, a missing parameter, an unknown function, two statements
  and a refused privilege are HTTP 400 (recorded).
- An error after some rows is HTTP 202. The body holds `data` with the rows
  that were sent, then `errors` (recorded). For example,
  `UNWIND [1, 2, 0, 4] AS x RETURN 10 / x` gives the rows 10 and 5, then
  `Neo.ClientError.Statement.ArithmeticError`.
- So HTTP 202 does not mean success. The driver must read `errors` at the end
  of each body.
- In JSON Lines, the error is an `Error` event after the `Record` events
  (recorded on 2026.09.0).
- An unsupported media type is HTTP 406 for `Accept` and HTTP 415 for
  `Content-Type` (recorded).

## Cancellation and timeouts

- The server does not stop a query when the client disconnects. A query that
  the client left after one second still showed `Running` in
  `SHOW TRANSACTIONS` a second later, on both releases (recorded). An
  earlier run showed it running for more than 9 seconds (measured by hand).
- Under D67, before D95, the tag started the statement. A statement that
  starts with a comment, such as `/* dbimp:c1 */`, keeps it in
  `currentQuery`. The ordinary user finds its own transaction with
  `SHOW TRANSACTIONS YIELD transactionId, currentQuery WHERE currentQuery
  STARTS WITH $tag`, and stops it with `TERMINATE TRANSACTION $id`, on both
  releases (recorded). After that, `SHOW TRANSACTIONS` lists nothing for the
  tag (recorded).
- A comment at the start moves each position of an error by its length, and
  the message shows it (found by `usql` on `v0.4.0`). A line comment at the
  end, as `\n// dbimp:c1`, is kept at the end of `currentQuery`, and
  `ENDS WITH $tag` finds it. `TERMINATE TRANSACTION` stops it, on both
  releases (measured by hand on 2026-09-29). An error in the text before it
  keeps its position: `RETURN 1 AS a,, 2` fails at line 1, column 15, and
  the message holds no comment (tested). An error at the end of the input,
  as in `RETURN 1 +`, points at the line of the comment (measured by hand).
  A statement that ends with a `//` comment of the caller still runs
  (measured by hand). D95 moves the tag to the end.
- After an early close, the server stops a statement by itself when it
  cannot write the next row. A statement that sent 5000 rows and then
  counted to 3000000000 still ran 2 seconds after the client left (measured
  by hand on 2026-09-29). A smaller answer is held back until the statement
  ends. In a transaction, the next statement fails with
  `TransactionAccessedConcurrently`, or the transaction is rolled back,
  while the answer of the last one is unread (tested). D105 says what
  `Rows.Close` does.
- `currentQuery` holds the whole text of a statement of 20,099 characters,
  with the tag at its end, and `ENDS WITH $tag` finds it, on both releases
  (measured by hand on 2026-09-29). So a long statement keeps its tag.
- `txMetadata` is HTTP 400 on 5.26.31, and accepted on 2026.09.0 (recorded).
  In a body of typed JSON, each value of `txMetadata` is typed JSON too.
  2026.09.0 refuses a plain string there with HTTP 400, and takes
  `{"$type": "String", "_value": ...}` (measured by hand).
  On 2026.09.0, each principal finds its own transaction with
  `SHOW TRANSACTIONS YIELD transactionId, metaData WHERE metaData.dbimp = $t`,
  and terminates it (recorded).
- A statement with the comment in an explicit transaction is found the same
  way. `TERMINATE TRANSACTION` ends the whole transaction, and the next
  request to it is HTTP 404 (recorded on both releases).
- `TERMINATE TRANSACTION` with an id of the wrong form gives a row and then
  `Neo.ClientError.General.InvalidArguments` on 5.26.31 (recorded).
- `maxExecutionTime` is ignored on 5.26.31: the slow query ran to its end. On
  2026.09.0, it stops the query with
  `Neo.ClientError.Transaction.TransactionTimedOutClientConfiguration` after
  the rows, with HTTP 202 (recorded).
- `maxExecutionTime` counts whole seconds. On 2026.09.0, the value 1 stopped
  the slow query after 1.1 seconds, and 2 stopped it after 3.9 seconds. The
  value 3000 let it run to its end after 5.4 seconds. The server takes 0.5
  and gives no error (measured by hand on 2026-09-29). So `WithTimeout`
  rounds its time up to the next second, and the test of the driver stops a
  statement with `WithTimeout(500*time.Millisecond)` (tested).
- `CALL db.info() YIELD name` gives `system` in a request to
  `/db/system/query/v2`, for both principals, on both releases (tested).
  So `WithDatabase` reaches another database through the path.
- `DELETE` of a transaction fails while a statement in it runs (measured by
  hand).

## Statements

- A request holds one statement. Two statements are HTTP 400 with "Expected
  exactly one statement per query but got: 2" (recorded).
- One `;` at the end of a statement is accepted (recorded). With the tag of
  D95 on the line after it, it is accepted too (measured by hand on
  2026-09-29, and tested).
- A comment, `//` to the end of the line or `/* */`, is accepted. A `$` or a
  `;` in a string literal is text (recorded).
- A line break in the statement, written as `\n` in the JSON string, is
  accepted (recorded).
- A comment before `CYPHER 5`, `EXPLAIN`, `PROFILE`, `USE` or a command of
  the system database leaves the statement working (recorded, with the
  comment at the start). With the tag of D95 at the end, `CYPHER 5`,
  `EXPLAIN` and `USE` work too (measured by hand on 2026-09-29).
- The server keeps the plan of a statement by its exact text. On 2026.09.0,
  a statement that took 153 ms to plan took 1 ms when it ran again with the
  same text. With a new comment, or one more space at the end, it took 10 ms
  to 16 ms each time (measured by hand).

## Principals

- The ordinary user of `dbrun` is `dbmeta_user`, with the role `publisher`
  (dbmeta D106). It reads and writes nodes and relationships (recorded).
- It is refused, with HTTP 400 and `Neo.ClientError.Security.Forbidden`: an
  index, a constraint, a database, an alias, a composite database, and
  `SHOW USERS` (recorded).
- It is refused impersonation of `neo4j`, which the administrator can do
  (recorded).
- It sees and terminates its own transactions (recorded).
- It reads the version with `CALL dbms.components()`, which gives the name,
  the versions and the edition (recorded). 2026.09.0 gives two rows. The row
  `Neo4j Kernel` gives the version, such as `5.26.31` or `2026.09.0`, and the
  edition `enterprise` (recorded). Both principals read that row through this
  driver (tested, by `TestIntegrationVersion`, which step 16 asks for).

## Flavors

- Memgraph and FalkorDB speak Bolt, and Amazon Neptune takes openCypher on
  an endpoint of its own. None of them is known to speak the Query API (not
  measured).

## Interfaces

`neo4j/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs RETURN 1, which checks the credentials and the database. |
| `driver.SessionResetter` | no | A transaction ends before database/sql hands the connection on, so nothing needs a reset. |
| `driver.Validator` | no | A connection holds no state on the server outside a transaction, so it is always valid. |
| `driver.NamedValueChecker` | yes | An argument keeps its Go value for typed JSON (D63), and a positional one fills $n (D64). |
| `driver.QueryerContext` | yes | The server binds each argument itself, through parameters. |
| `driver.ExecerContext` | yes | Exec reads the result to its end, and has no count of rows (D66). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, bound each time. |
| `driver.ConnBeginTx` | yes | An explicit transaction of the Query API (D65), which keeps the context of BeginTx (D69). |
| `driver.RowsColumnScanner` | yes | A value is decoded from typed JSON when it is scanned (D63). |
| `driver.RowsNextResultSet` | no | A request holds one statement, so a response has one result. |
| `driver.RowsColumnTypeScanType` | no | No type arrives for a column, and each value names its own type. |
| `driver.RowsColumnTypeDatabaseTypeName` | no | No type arrives for a column, and each value names its own type. |
| `driver.RowsColumnTypeLength` | no | No type arrives for a column, so no column has a length. |
| `driver.RowsColumnTypeNullable` | no | No type arrives for a column, and any value can be NULL. |
| `driver.RowsColumnTypePrecisionScale` | no | No type arrives for a column, so no column has a precision or a scale. |
<!-- /dbimp:interfaces -->

## Faults

`usql` has no driver for Neo4j, so there are no faults to carry over.

## Second opinions

Gemini and DeepSeek were asked on 2026-09-27 about what step 6 did not find.
Each lead was then sent to both releases.

- Whether an ordinary user can terminate its own transaction on 5.26.
  DeepSeek said yes. Gemini said that it needs the privilege
  `TERMINATE TRANSACTION`. The server showed that DeepSeek was right, on both
  releases (recorded).
- Whether a comment in the statement finds it in `SHOW TRANSACTIONS`.
  DeepSeek said yes, and the server agreed (recorded).
- Whether a server names its media types. Both said no, and the discovery
  document agreed (recorded).
- How a plain JSON number is read. Both said that `1` is an integer, `1.0`
  and `1e3` are floats, and an integer up to the largest int64 is exact. The
  server agreed (recorded). Gemini said that a larger one fails, and it is
  HTTP 400 (recorded).
- Whether a typed parameter can be a `Node`. Gemini said no, and DeepSeek
  said yes, as a reference. The server refused it (recorded).
- A limit on the body. DeepSeek said 4 MiB with HTTP 413, and Gemini said
  about 100 MB with HTTP 413. Both were wrong. The limit is on one string of
  20,000,000 characters, with HTTP 400 (measured by hand).
- DeepSeek said that 2026.x stops a query when the client disconnects. The
  server showed that it does not (recorded).
- Gemini said that a result that hits a memory limit ends the body early
  with HTTP 202 (not measured).
- Both said that the Query API has no paging but `SKIP` and `LIMIT` (not
  measured).

## Open questions

None. Ken accepted the decisions of step 9, D60 to D69 in
[decisions/](decisions/README.md), on 2026-09-27, and D95 and D100 on
2026-09-29. He decided the questions of the review of D97 on 2026-09-29, in
D105, D109, D110, D112 and D114.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-09-29 from the staged code, which holds D95 and D100. A fact of
Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and a fact of Neo4j from
the sections above.

### The server

| | Couchbase | Neo4j |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /db/<database>/query/v2`, with `statement` and `parameters` |
| Database | The key `query_context` of the body | The path of each request (D61) |
| Language | SQL++, which is close to SQL | Cypher |
| DDL | In SQL++ | In Cypher, for an index, a constraint and a database |
| Parameters | `?`, `$1` and `$name` | `$name` only, where a name can be a number, such as `$1` |
| Framing | One body for the whole result, which does not page | One body, `fields` and then `values`, sent in chunks for a large result |
| Columns | `signature`, before the first row | `fields`, before the first row |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | Can come with HTTP 202, in `errors` after the rows |
| Types | JSON. No date, decimal, UUID or binary | Typed JSON, with each temporal type, a point, bytes, a node, a relationship and a path, and a UUID on 2026.09.0 |
| Cancel | The server stops a query when the client leaves | The query runs on when the client leaves. `TERMINATE TRANSACTION` stops it |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | The endpoints `/tx`, `/tx/<id>`, `/tx/<id>/commit`, and `DELETE` to roll back |
| Several statements | Refused | Refused |
| Authentication | Basic, or `creds` in the body | Basic |
| Default port | 8093, or 18093 with TLS | 7474, or 7473 with TLS |

The differences that a caller sees:

- The columns and their order come from `fields`, as the statement names
  them (D66).
- An error after the rows arrives with HTTP 202, so the driver reads
  `errors` at the end of each body (D66).
- The server does not stop a query when the client leaves, so the driver
  stops it as `cancel` says (D67 and D95).

### The driver

| | `couchbase` | `neo4j` |
| --- | --- | --- |
| Size, without tests, on 2026-09-29 | About 1300 lines in 8 files | About 2800 lines in 11 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Database`, `Cancel` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithCancel`, through `WithOptions` or an argument. `BeginTx` takes them through `WithOptions` only (D109). `WithParameter` sets any key of the body. `WithTimeout` gives `dbimp.ErrNotSupported` before 2026.04 |
| Arguments | Sent to the server as `args` and `$name` | Sent in `parameters` as typed JSON. Ordinal n fills `$n` (D64). A struct, a node, a relationship, a path and an `apd.Decimal` are refused |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | `dbimp.ArrayRows` from the root package, over `values` |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | None |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44). A time and a UUID are strings | `int64`, `float64`, `[]byte`, `uuid.UUID`, `time.Time`, and the types `Date`, `LocalTime`, `Time`, `LocalDateTime`, `Duration`, `Point`, `Node`, `Relationship`, `Path` and `Vector` (D63) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported` (D66) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` begins a transaction on the endpoints (D65). `ReadOnly` sends `accessMode: READ`. An error on the server ends the transaction, and `Commit` then returns that error. `Rollback` after the context ends still ends the transaction on the server (D100) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | `cancel=tag`, `cancel=metadata` or `cancel=none` (D67 and D95). An early `Close` stops a statement that runs on, and in a transaction reads the rest of the answer (D105) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code int, Msg}` | `*ResponseError`, with the HTTP status and a list of `Error{Code string, Message}` |
| Authentication | Basic | `auth=basic`, or `auth=bearer`, which the server of dbrun refuses (D116) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, the Go types of D63, and `CancelTag`, `CancelMetadata` and `CancelNone` |

The differences that a caller sees:

- `Rollback` after the context of `BeginTx` ends still ends the
  transaction on the server (D100), where Couchbase sends nothing (D45).
- `RowsAffected` returns an error, because the counters of Neo4j count
  nodes, relationships and properties, and none of them counts rows (D66).
- The columns have no database type, and the scan type of each is `any`,
  because no type arrives for a column, and each value names its own type
  (the table of interfaces above).
- A value keeps its type through typed JSON, where Couchbase gives JSON
  shapes (D63).
- `cancel=tag` changes the text of each statement, which shows in
  `SHOW TRANSACTIONS` (D95).
- An early `Close` in a transaction reads the rest of the answer, where
  Couchbase reads nothing more (D105).
- Ken decided these on 2026-09-29:
  - The driver takes the four options that D109 gives every driver, and
    `WithCancel` for the key `cancel`. W15 of [BACKLOG.md](BACKLOG.md)
    added them.
  - The driver has no `IsValid`, and `CheckNamedValue` calls `Value` of a
    `driver.Valuer` itself and refuses a struct (D112). It needs no
    `ResetSession` (D102).
  - A list or a map scanned into a `*[]byte` or a `*jsontext.Value` does
    not get its JSON text, which only Couchbase gives (D114).
  - The driver gets the key `auth=basic|bearer` in W14 (D110).
