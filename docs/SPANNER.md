# Google Cloud Spanner

This file holds what is known about Google Cloud Spanner over its REST API, for the
driver `spanner` (W38, D185, D187 and D191). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with how and when, or "not measured", with its source.

Step 6 recorded the hosted service on 2026-10-10 in two passes. The first pass sent
260 requests. The second pass sent 405 requests, with the same request names and a
longer script, and it replaced the first. This file holds the second pass. The main
session sent the requests as one service account, and the recorder replaced the
Authorization header with `REDACTED`. The recordings are under `testdata/spanner/`,
and `testdata/spanner/requests.json` is the script. The recorder runs the setup
first and the teardown last, so the file numbers follow that order and not the
order of the script. The project id in the recordings is the redacted name
`dbimp-project`. The instance is `dbimp-spanner` and the database is `dbimp_test`.
The service account holds `roles/spanner.databaseAdmin` on that one database. Every
table that the script makes starts with `dbimp_t_`, and the teardown at the end of
the script drops them.

Four things in the second pass did not work as planned, and the recordings show
them:

- The request "the same stream resumed with the token of its first message" carries
  the text `{{ert}}` where a token belonged, because the first message of that stream
  has no `resumeToken`. The server answered HTTP 400 for the text. The request proves
  nothing about a resume. The resume is settled by "a stream resumed with the token".
- The recording of "a response with Accept-Encoding gzip on a large result" has the
  header `Content-Encoding: gzip` and `Content-Length: 5155`, and no body. The
  recorder keeps no gzip body.
- The table of "a table with an INTERVAL column, a FLOAT32 column and an array of
  INTERVAL" was refused in its operation. So the requests that fill and read that
  table ("insert the rows of the INTERVAL table", "the rows of the INTERVAL table",
  "the rows of the INTERVAL table on the stream") answer "Table not found" and prove
  nothing about a value. The `INTERVAL`, `FLOAT32` and old timestamp forms come from
  literals instead ("INTERVAL values with fractions and negatives", "FLOAT32 of 0.1
  and FLOAT64 of 0.1", "TIMESTAMP with a fraction below a microsecond and before
  1970").
- The wrong token was accepted. See "A request with a wrong token" under Requests.

## Summary

- Product: Google Cloud Spanner, a hosted, distributed SQL database. Name in `dbrun`:
  none that serves REST today. `dbrun` starts `spanner-2026.r4-lts`, which is
  Spanner Omni and serves gRPC only. See "Measured on Omni" under Requests.
  `dbmeta` is switching back to the Cloud Spanner emulator, which serves REST on
  port 9020, and the entry does not exist yet (D187, source: Ken, 2026-10-10, not
  measured).
- R: not met today. No `dbrun` release serves the REST API of Spanner. The hosted
  service needs a Google Cloud project with billing and a service account, so CI
  cannot test it without a secret. The Cloud Spanner emulator will meet R when
  `dbmeta` has its entry (D187). The measurement is then repeated on it.
- H: met on the hosted service. `https://spanner.googleapis.com/v1/...` takes HTTP
  and JSON, and it answered every statement that the script sent (recorded: "a
  statement with no transaction", "a statement on the stream"). It needs no
  binary encoding and no gRPC.
- S: met. The dialect is GoogleSQL (recorded: "getDatabase" has
  `databaseDialect` `GOOGLE_STANDARD_SQL`). The service answered `SELECT`, `INSERT`,
  `UPDATE`, `DELETE`, `INSERT OR UPDATE`, `INSERT OR IGNORE`, `THEN RETURN`,
  `INFORMATION_SCHEMA` and `SPANNER_SYS` (recorded: "an insert in the transaction",
  "INSERT OR UPDATE of a new row", "an insert with THEN RETURN", "the tables", "a
  SPANNER_SYS table"). It refused `MERGE` and `TRUNCATE TABLE` (recorded: "MERGE", "a
  TRUNCATE statement"). The DDL goes to another call, `updateDatabaseDdl`, and
  `executeSql` refuses it (recorded: "a CREATE TABLE statement sent to executeSql").
- Whether it can be a driver: yes, on REST and JSON. No condition of "When it cannot
  be a driver" failed.
  - The columns come first. `metadata.rowType.fields` is in the first message of
    every result, even a result with no rows (recorded: "columns of a result with no
    rows", "the columns of the same query on the stream"). A stream that a
    `resumeToken` restarts has no `metadata` (recorded: "a stream resumed with the
    token").
  - A result can be read one message at a time. `executeStreamingSql` returns a JSON
    array of `PartialResultSet` messages (recorded: "a statement on the stream"). The
    recorder keeps the whole body, so the time of each message is not measured. The
    server did send HTTP 200 before a later error, so it sends the first messages
    before it ends the query (recorded: "a division by zero in row 3500 of 4000,
    unsorted, on the stream").
  - `executeSql` has no paging member (recorded: "a statement with a pageSize"). It
    answered 5000 rows whole (recorded: "5000 rows with UNNEST") and a string of 3 MB
    (recorded: "big: a string of 3 MB with executeSql"). It refused twelve rows of
    1 MB with HTTP 400 `FAILED_PRECONDITION` and the text "Result sets larger than
    10.00M can only be yielded through the streaming API" (recorded: "big: twelve rows
    of 1 MB with executeSql"). The stream carried the same twelve rows (recorded:
    "big: twelve rows of 1 MB on the stream"). So the driver reads every result with
    `executeStreamingSql`. The limit of a stream was not reached.
  - The condition most at risk was one row at a time, because of `chunkedValue`. The
    recordings now show it. The server splits a value of more than about 1 MiB across
    messages, and the driver joins the pieces as text (recorded: "big: a string of 3 MB
    on the stream", "big: a BYTES value of 2 MB from a table on the stream", "big:
    twelve rows of 1 MB on the stream"). See Responses.
  - The risk that remains is an error after some rows. The server answered HTTP 200,
    sent the rows, and then sent an element that holds `error`. The last message
    before the error held a row that was not complete (recorded: "a division by zero in
    row 3500 of 4000, unsorted, on the stream"). The driver must drop that row.
  - The login is the third cost. Every request needs a Bearer token that comes
    from a signed JWT (see Requests), and a session comes before a statement.
- Scheme in `dburl`: `Name` is `spanner`, the alias is `sp`, the generator is
  `GenSpanner`, the transport is `TransportUnix`, the deployment is hosted and the
  `Dialect` is `spanner`. `GoPackage` is `github.com/xo/dbimp/spanner`. The URL is
  `spanner://host:port/project/instance/database`. It keeps its user and its
  query. An empty host leaves the endpoint to the driver. The path must name the
  project, the instance and the database (read of `dburl/scheme.go` and
  `dburl/dsn.go`, 2026-10-10). The generator is provisional (D60).
- Driver of `usql` now: `github.com/googleapis/go-sql-spanner` v1.26.0, which uses
  `cloud.google.com/go/spanner` v1.95.1 and gRPC (read of `usql/go.mod` and
  `usql/drivers/spanner/spanner.go`, 2026-10-10). `usql` lists its cost as the
  Google Cloud SDK, gRPC and the protos of Envoy (read of `usql/docs/BACKLOG.md`
  item 17.4, 2026-10-10). The driver sets no hook for the version.
- What `dbmeta` has: the container entry for Omni, and a model with a version query
  that reads the highest optimizer version, `SELECT CAST(MAX(version) AS STRING) AS
  version FROM spanner_sys.supported_optimizer_versions` (read of
  `dbmeta/models/spanner/spanner.go`, 2026-10-10). Spanner has no version function
  (recorded: "a version function"), and the query of `dbmeta` answered `"9"` on the
  hosted service (recorded: "the greatest optimizer version").

## Requests

All of this is recorded on the hosted service on 2026-10-10 unless it says otherwise.

- The host is `spanner.googleapis.com`, over HTTPS. The paths start with `/v1/`. A path
  with no version answers HTTP 404 with an HTML page (recorded: "a path with no
  version"). A method that the path does not have answers the same page (recorded:
  "a statement with PUT").
- The name of a database is `projects/{project}/instances/{instance}/databases/{database}`.
  A session is `{database}/sessions/{id}`. An operation is
  `{database}/operations/{id}`. The verbs that follow a name come after a colon, such
  as `{session}:executeSql`.
- The calls that the script used, with the method and the path after `/v1/`:
  - `POST {database}/sessions` is `createSession` (recorded: "createSession with no
    body"). `POST {database}/sessions:batchCreate` is `batchCreateSessions` (recorded:
    "batchCreateSessions"). `GET {session}` is `getSession`. `GET {database}/sessions`
    is `listSessions`. `DELETE {session}` is `deleteSession`.
  - `POST {session}:executeSql`, `:executeStreamingSql`, `:executeBatchDml`, `:read`,
    `:streamingRead`, `:beginTransaction`, `:commit`, `:rollback`, `:partitionQuery`
    and `:partitionRead`.
  - `PATCH {database}/ddl` is `updateDatabaseDdl`, and it answers an operation.
    `GET {database}/ddl` is `getDatabaseDdl`. `GET {database}/operations/{id}` reads an
    operation, `GET {database}/operations` lists them, and `POST
    {database}/operations/{id}:cancel` cancels one. `DELETE {database}/operations/{id}`
    answers HTTP 501 (recorded: "delete a finished operation"). `GET {database}` is
    `getDatabase`. `GET {database}/databaseRoles` lists the roles.
- The body of a request is JSON. A request with no `Content-Type` header was accepted
  (recorded: "a statement with no content type"). A member that the message does not
  have answers HTTP 400 `INVALID_ARGUMENT`, with the name of the member (recorded: "a
  statement with a field that does not exist"). A body that is not JSON answers HTTP
  400 (recorded: "a statement with a body that is not JSON"). An empty body answers
  HTTP 400 that names the field `sql` (recorded: "a statement with an empty body").
- The answer is JSON with `Content-Type: application/json; charset=UTF-8`. Its
  headers name the server `ESF`. The body of the answers is indented JSON.
- A request that sets `Accept-Encoding: gzip` got `Content-Encoding: gzip` and a body
  of 5155 bytes for 2000 rows (recorded: "a response with Accept-Encoding gzip on a
  large result"). The recording keeps no gzip body, so the size of the text before
  compression is not measured. A request that sets `Content-Encoding: gzip` with a
  body that is not gzip answered HTTP 200 (recorded: "a statement with a gzip content
  encoding and a body that is not gzip"), so the recording does not show whether the
  server reads a gzip request. The recording shows no redirect and no `Retry-After`
  header.
- Authentication, measured by the main session on 2026-10-10 with a small program
  (`mkgtoken`) that is not in this repository:
  1. The key of the service account is a JSON file with `client_email`,
     `private_key` (PKCS 8, RSA) and `token_uri`.
  2. The program signs a JWT with RS256. The header is `{"alg":"RS256","typ":"JWT"}`.
     The claims are `iss` (the `client_email`), `scope`, `aud` (the `token_uri`), `iat`
     and `exp` (55 minutes later).
  3. It sends `POST` to `https://oauth2.googleapis.com/token` as a form with
     `grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer` and `assertion`. The
     answer holds `access_token` and `expires_in`.
  4. Each request then sends `Authorization: Bearer {access_token}`.
  The scopes were `https://www.googleapis.com/auth/spanner.data` for the statements and
  `https://www.googleapis.com/auth/spanner.admin` for the DDL (source: the brief of
  the main session). The answer of the token endpoint is not recorded, so
  `expires_in` and the error of a bad key are not measured. Everything that this
  step needs is in the standard library: `crypto/rsa`, `crypto/sha256`, `net/http`.
- A request with a wrong token. The recorder adds `-wrong` to the end of the token
  (the test of `dbimptest/cmd/record` checks the header), and the recording redacts
  the header. The server accepted that token with HTTP 200 on seven calls: "createSession
  with a wrong token", "a statement with a wrong token", "getSession with a wrong
  token", "a statement on the stream with a wrong token", "getDatabase with a wrong
  token", "listSessions with a wrong token" and "getDatabaseDdl with a wrong token".
  So the server does not read the whole token, and this wrong token proves nothing
  about a refusal.
  - A wrong `access_token` in the query, with the right header, answered HTTP 200
    (recorded: "getSession with a wrong access_token in the query"). The query did
    not replace the header.
  - A wrong `key` in the query answered HTTP 400 `INVALID_ARGUMENT` with an
    `ErrorInfo` whose `reason` is `API_KEY_INVALID` (recorded: "getSession with a wrong
    key in the query").
  - A token that is not a token at all, an expired token, a token with the wrong scope
    and a request with no header: not measured. The recorder has one token and always
    sets the header.
- Every statement needs a session in its path. See Transactions for the sessions.
- A request to a database that does not exist answered HTTP 403, not 404 (recorded:
  "createSession on a database that does not exist"). See Errors.

### Measured on Omni

The earlier measurement of 2026-10-10 on the container `spanner-2026.r4-lts`
(Spanner Omni, image `us-docker.pkg.dev/spanner-omni/images/spanner-omni`, started
with `start-single-server`) holds. It is not the Cloud Spanner emulator (dbmeta D215).
The entry published one port, 15000 inside the container, and `dbrun dsn --json` gave a
`dsn` and a `url` and no `api` address. It was measured with `curl`, and no recorded
file exists.

- HTTP/1.1 `GET /v1/projects/default/instances`: curl stops with "Received HTTP/0.9
  when not allowed". The bytes start with `00 00 24 04`, an HTTP/2 SETTINGS frame.
- HTTPS to the same port: "wrong version number". The port has no TLS.
- HTTP/2 without TLS, for `GET` and `POST` with JSON on `/`, `/healthz`,
  `/v1/projects/default/instances`, `/v1/projects/default/instanceConfigs`, the sessions
  of a database and `sessions:batchCreate`: the server resets the stream with
  `INTERNAL_ERROR`.
- HTTP/2 `POST /google.spanner.v1.Spanner/CreateSession` with `content-type:
  application/grpc`: HTTP 200, `grpc-status: 1`, and the header
  `x-spanner-omni-license-bin`. The port is the gRPC endpoint. With
  `application/grpc-web+proto` the stream is reset.
- The `dbmeta` session measured the other ports of Omni from inside its container.
  Omni listens on 15000 to 15012, 15014, 15015 and 15025. Every port except 15012
  refuses plain HTTP/1.1. The port 15012 answers 200 with an HTML page titled
  "Spanner" on `/` and 404 on the paths of the REST API. Omni has no REST flag
  (reported by `dbmeta`, not measured here).
- The classic emulator serves REST on port 9020 and gRPC on port 9010 (source: the
  documentation of the emulator, not measured). Whether it answers as the hosted
  service does is not measured.

## The DSN

The URL that `dburl` writes is `spanner://host:port/project/instance/database` (see
Summary). The facts that a driver needs:

- The project, the instance and the database are the three parts of the path.
  The project id of the hosted service is lower case letters, digits and hyphens, and
  it can hold a dot or a colon in a domain scoped project (source: the regular
  expression `dsnRegExp` in `go-sql-spanner` v1.26.0, read 2026-10-10).
- The endpoint is `spanner.googleapis.com` on HTTPS. The emulator is on `localhost`
  and needs HTTP with no TLS, and no credential (source: `go-sql-spanner`
  `usePlainText`, read 2026-10-10, and the documentation of the emulator, not
  measured).
- The credentials are the key file of a service account, or an access token that
  another tool made. The key file is JSON with a private key, so it cannot be a part
  of a URL without breaking D94. The choices are a path in the URL, the text of
  the key in a query key, or an environment variable. See Open questions.
- `go-sql-spanner` takes the name of the database as the DSN, with the host first
  and parameters after a `;` or a `?`, such as
  `localhost:9010/projects/p/instances/i/databases/d;usePlainText=true` (source: the
  comment of `driver.go` in v1.26.0, read 2026-10-10).
- Examples that `dburl` generates: `spanner:///p/i/d` for the hosted service, and
  `spanner://localhost:9020/p/i/d` for the emulator (read of `GenSpanner`, 2026-10-10).

## Responses

- `executeSql` answers one `ResultSet`: `metadata`, `rows` and optionally `stats`
  (recorded: "a statement with no transaction"). The member `rows` is missing when the
  result has no row (recorded: "columns of a result with no rows"). A row is an array
  of values in the order of the columns.
- `executeStreamingSql` and `streamingRead` answer a JSON array of `PartialResultSet`
  messages (recorded: "a statement on the stream", "a streaming read"). Each message has
  `values`, which is a flat list of the values of many rows, and `last` on the last
  message. The first message has `metadata`. A message of a stream is not a row.
  - The values of rows are not grouped. The result of `SELECT 1 AS a, 'x' AS b` is
    `["1","x"]`, and the result of two rows is four values in one list (recorded:
    "the rows of every type on the stream", with 60 values for 3 rows of 20 columns).
    The driver cuts the list by the number of columns.
  - A result of 5000 rows of one column arrived in one message of 5000 values with
    `last` (recorded: "5000 rows on the stream").
  - The cut of a long result into messages is not fixed. One statement, 5000 rows of
    a number and a string of 200 characters (about 1 MB), was sent twice. The first
    answer held 9810 values and `chunkedValue: true` in the first message and 191
    values and `last` in the second, with no `resumeToken` (recorded: "5000 wide rows
    on the stream"). The second answer held 9600 values and a `resumeToken` in the
    first message and 400 values and `last` in the second (recorded: "a stream to take
    a resume token from"). The first pass cut the same statement in a third way, with
    9760 values and a `resumeToken` in the first message, and 240 values in the second.
  - `chunkedValue: true` on a message means that its last value continues in the first
    value of the next message. It is recorded for text and not for a list.
    - The first message of "5000 wide rows on the stream" ends in the middle of the
      string of row 4905. The last value has 195 characters and the first value of
      the next message has 5, which makes 200.
    - A string of 3 MB (3,000,000 characters) arrived in three messages of one value
      each, of 1,048,561, 1,048,576 and 902,863 characters. The first two have
      `chunkedValue: true` (recorded: "big: a string of 3 MB on the stream"). A string
      of 1,000,000 characters arrived in one message with no chunk (recorded: "a result of
      one string of 1 MB on the stream"). So a piece has at most 1,048,576
      characters.
    - A `BYTES` value of 2,000,000 bytes is base64 text of 2,666,668 characters, and it
      arrived in three messages of 1,048,546, 1,048,576 and 569,546 characters
      (recorded: "big: a BYTES value of 2 MB from a table on the stream", "big: a
      streaming read of the BYTES value of 2 MB"). The first piece is not a multiple of
      four, so the driver must join the text and then decode it. Decoding each piece alone
      is wrong.
    - Twelve rows of a number and a string of 1,000,000 characters arrived in twelve
      messages. A message can hold the end of one value, whole values and the start of
      the next value (recorded: "big: twelve rows of 1 MB on the stream", whose
      messages hold 4, 3, 3, 3, 1, 4, 3, 3, 3, 1, 4 and 1 values). The join of the
      pieces gave twelve rows of 1,000,000 characters. The join is
      `value[last] = value[last] + next.values[0]`, with the rest of the next values
      appended, and it repeats while a message has `chunkedValue: true`.
    - The joining of two lists, which the protocol allows, is not recorded. The source
      says that two lists join with the last element of the first and the first element of
      the second joined too, when they are strings or lists (source:
      `partialResultSetDecoder.merge` in `cloud.google.com/go/spanner` v1.95.1, read
      2026-10-10).
  - `resumeToken` is a base64 string of about 13 KB (12,952 characters in "a stream to
    take a resume token from", 14,072 in the twelve rows). It sits in a message that ends
    on a whole value, never in a message with `chunkedValue: true`. A message that
    carries one can hold a single value, which completes a row (recorded: "big: twelve
    rows of 1 MB on the stream").
  - A resume works. The same statement, sent again on the same session with the
    `resumeToken` of the first message, answered one message with the 400 values that
    follow the first message (rows 4801 to 5000) and `last`. Its values are equal to the
    second message of the first answer. It has no `metadata`, so the driver keeps the
    columns of the first answer (recorded: "a stream resumed with the token"). A
    token that is base64 text and not a token answers HTTP 400 `INVALID_ARGUMENT`, with
    a `BadRequest` that names the field `resume_token` and says "Got: notatoken", in an
    array of one element (recorded: "a stream resumed with a token that is not valid").
    Text that is not base64 answers HTTP 400 "Base64 decoding failed" (recorded: "the
    same stream resumed with the token of its first message", which sent `{{ert}}`).
    A resume from the token of a later message, and a resume that is cut by a network
    error, are not measured.
  - A stream of one row is one message with `metadata`, `values` and `last` (recorded:
    "a statement on the stream"). A stream of no row is not recorded.
  - An error before the first row has HTTP 400 and an array of one element that holds
    `error` (recorded: "a division by zero after 5000 rows on the stream", whose query
    sorts, so no row came first). An error after rows has HTTP 200. The array holds the
    messages that were sent, and then an element that holds `error` with `code` 400
    (recorded: "a division by zero in row 3500 of 4000, unsorted, on the stream"). The
    last message before that error had 5999 values and `chunkedValue: true`, so
    its last value was cut and the row was not complete. That message had no
    `resumeToken`, so no resume from that point was possible. The same query on
    `executeSql` answered HTTP 400 and no row (recorded: "a division by zero in row 3500
    of 4000, unsorted"). So the driver reads each element, and it takes an element
    with `error` as the end of the result with that error, whatever the HTTP status
    was.
  - A DDL statement on the stream answers HTTP 400 in an array of one element
    (recorded: "a CREATE TABLE statement sent to executeStreamingSql").
- The columns come from `metadata.rowType.fields`. Each field has `name` and `type`.
  The `name` is missing when the column has no name, such as `SELECT 1` (recorded: "a
  statement with a wrong token", whose field has only a `type`). Two columns can have
  one name (recorded: "columns that share a name"). The type has `code`, and an array
  has `arrayElementType`, and a struct has `structType.fields` (recorded: "ARRAY of
  STRUCT"). The first message always has the columns, with rows or not.
- `metadata.transaction` holds the id of a transaction that an inline `begin`
  made (recorded: "a statement that begins a transaction inline"), or the read
  timestamp of a single use read (recorded: "a single use read with exact staleness"),
  or `{}`.
- `metadata.undeclaredParameters` lists the parameters that the statement used and
  `paramTypes` did not declare (recorded: "a parameter with no paramTypes").
- A DML statement answers `metadata.rowType` of `{}` and `stats.rowCountExact`, a
  string such as `"1"` (recorded: "an insert in the transaction"). A statement
  that changes no row answers `"0"` (recorded: "an update that changes no row"). A
  partitioned DML statement answers `rowCountLowerBound` (recorded: "a partitioned DML
  statement"). A DML statement with `THEN RETURN` answers the columns and the rows
  with `rowCountExact` (recorded: "an insert with THEN RETURN", "an update with THEN
  RETURN and many rows").
- `queryMode` `PLAN` answers `stats.queryPlan` and no row, `PROFILE` answers the plan
  with `executionStats` for each node, and `WITH_STATS` answers `stats.queryStats`
  with strings such as `"elapsed_time": "3.56 msecs"` (recorded: "queryMode PLAN",
  "queryMode PROFILE", "queryMode WITH_STATS").
- A result has no paging. A request that sets `pageSize` answers HTTP 400 (recorded: "a
  statement with a pageSize"). A `LIMIT` works (recorded: "a query with a LIMIT of 3").
- The size of a result:
  - `executeSql` answered a string of 3,000,000 characters, a body of 3,000,006 bytes
    (recorded: "big: a string of 3 MB with executeSql"). It answered HTTP 400
    `FAILED_PRECONDITION` for twelve rows of 1,000,000 characters, and the message is
    "Result set too large. Result sets larger than 10.00M can only be yielded through the
    streaming API." (recorded: "big: twelve rows of 1 MB with executeSql"). So the limit
    of `executeSql` is between 3 MB and 12 MB, and the message names 10.00M. The exact
    edge is not measured.
  - The stream answered the same twelve rows, which are 12 MB, and a `BYTES` value of 2 MB
    in base64 (recorded: "big: twelve rows of 1 MB on the stream", "big: a BYTES value
    of 2 MB from a table on the stream"). It reached no limit. The limit of 100 MB for
    a row and 10 MB for a column value is from the documentation and is not measured
    (source: the REST reference of `executeStreamingSql`).
  - The 11 MB value of the first pass failed in `REPEAT`, a function that refuses an
    output above 1 MB with HTTP 400 `OUT_OF_RANGE` and "Output of REPEAT exceeds max
    allowed output size of 1MB" (recorded: "a result of one string of 11 MB"). It was
    never a limit of the response. The big values of the second pass use
    `ARRAY_TO_STRING` and rows that a table holds.
  - A table held twelve rows of 1,000,000 characters and a row with a `BYTES` value of
    2,000,000 bytes (recorded: "big: the size of the rows"). The limit of a `STRING(MAX)`
    column is not measured.
- Lists, such as `listSessions` and the list of operations, page with `pageSize`,
  `pageToken` and `nextPageToken` (recorded: "listSessions with a wrong token", with a
  `pageSize` of 1, and "the list of the operations of the database", with a `pageSize`
  of 5).
- A response with no content is `{}` (recorded: "deleteSession", "rollback", "cancel the
  running operation"). A list with no item is `{}` too (recorded: "listSessions after the
  deletes").
- Compression: see Requests. Redirects: none recorded.

## Types

Each type is a `code` in `metadata.rowType.fields[].type`. The recorded values come
from the table `dbimp_t_types` and from literals.

- `INT64` is a decimal string, such as `"9223372036854775807"` and
  `"-9223372036854775808"` (recorded: "INT64 limits", "the rows of every type"). A
  JSON number is refused as a parameter (recorded: "an INT64 parameter given as a JSON
  number"). The range is 64 bits.
- `FLOAT64` is a JSON number, such as `1.5`, `1e+308`, `4.94065645841247e-324`, `-0` and
  `0.30000000000000004` (recorded: "FLOAT64 values"). `NaN` is the string `"NaN"` and the
  infinities are `"Infinity"` and `"-Infinity"` (recorded: "FLOAT64 NaN and infinity").
  A parameter takes the same three strings (recorded: "FLOAT64 parameters that are not
  finite").
- `FLOAT32` is a JSON number that holds the value as a `float64`, so the text shows the
  digits that a `float32` cannot hold. `CAST(0.1 AS FLOAT32)` arrives as
  `0.10000000149011612`, and `CAST(16777217 AS FLOAT32)` arrives as `16777216` (recorded:
  "FLOAT32 of 0.1 and FLOAT64 of 0.1"). `1.5` arrives as `1.5`, and a not a number
  value arrives as the string `"NaN"` (recorded: "the rows of every type", "ARRAY of
  UUID and FLOAT32"). The number is exactly the widened `float32`, so a driver that reads
  it as a `float64` and converts it to a `float32` gets the original value back. The
  value `3.4028235e+38`, the largest `float32`, was not read back, because the table of
  that request was refused.
- `NUMERIC` is a string with a scale of 9 at most and 29 digits before the point, such
  as `"99999999999999999999999999999.999999999"`, its negative, `"0.000000001"` and `"1"`
  (recorded: "NUMERIC values"). The text drops the trailing zeros.
- `BOOL` is a JSON boolean (recorded: "BOOL values").
- `STRING` is UTF-8 text. It keeps an empty string, a newline and non ASCII text
  (recorded: "STRING values"). A string of 3,000,000 characters worked as the value of
  an expression (recorded: "big: a string of 3 MB with executeSql"). The limit of a
  string in a column is not measured.
- `BYTES` is base64 text, such as `"YWJj"` for `abc`, `"AP8="` for `00 ff`, and `""` for an
  empty value (recorded: "BYTES values"). A value of 2,000,000 bytes arrived in three
  pieces on the stream (see Responses).
- `DATE` is `"2024-01-02"`, from `"0001-01-01"` to `"9999-12-31"` (recorded: "DATE
  limits").
- `TIMESTAMP` is RFC 3339 text in UTC with a `Z`, with up to nine digits of fraction, and no
  trailing zeros in the fraction. The recorded forms are `"2024-01-02T03:04:05.123456789Z"`,
  `"0001-01-01T00:00:00Z"`, `"9999-12-31T23:59:59.999999999Z"` and `"2024-01-02T03:04:05Z"`
  (recorded: "TIMESTAMP values"). A value before the epoch has the same form:
  `"1969-12-31T23:59:59.5Z"`, `"1969-07-20T20:17:40.123456789Z"`,
  `"1900-01-01T00:00:00.000000001Z"` and `"1969-12-31T23:59:59.999999999Z"` (recorded:
  "TIMESTAMP values", "TIMESTAMP with a fraction below a microsecond and before 1970").
  Nanoseconds before 1970 are exact. A zone in the input changes the instant and is not
  in the output (recorded: "TIMESTAMP with a zone", which gave `"2024-01-01T21:34:05Z"`).
  The stream gives the same text (recorded: "the literals of the stream: TIMESTAMP
  values"). A mutation takes the text `"spanner.commit_timestamp()"` for a column with
  `allow_commit_timestamp`, and a DML statement takes `PENDING_COMMIT_TIMESTAMP()`
  (recorded: "a mutation that writes the commit timestamp", "a DML statement with the
  pending commit timestamp"). The value of the DML statement then reads as the commit
  timestamp of its transaction (recorded: "the rows after the DML forms", "commit the DML
  forms with maxCommitDelay").
- `JSON` is a string that holds JSON text, such as `"{\"a\":1}"`. The JSON value `null`
  arrives as the text `"null"`, and a SQL NULL arrives as `null`. A number of 20 digits
  stayed exact in the text, as `"12345678901234567890"`, and `1.0` stayed `"1.0"`
  (recorded: "JSON values", "the rows of every type").
- `UUID` is a lower case string, such as `"f47ac10b-58cc-4372-a567-0e02b2c3d479"`
  (recorded: "UUID values"). The type is a column type (recorded: "add a column").
- `INTERVAL` is ISO 8601 text. It is a type of an expression and a parameter, and a
  column cannot have it. Each part has its own sign, and a sign comes after `T` for a
  time part.
  - The recorded forms are `"P1D"`, `"P-5M"`, `"P1Y2M3DT4H5M6S"`, `"P1Y"`, `"P-1D"`,
    `"P-1Y-2M-3DT-4H-5M-6S"`, `"PT1.5S"`, `"PT-1.5S"`, `"PT0.000001S"` and `"P0Y"` for
    zero (recorded: "INTERVAL values", "INTERVAL values with fractions and negatives").
  - A parameter takes `"P1Y2M3DT4H5M6S"` (recorded: "the parameter INTERVAL alone").
  - A table with a column of the type `INTERVAL`, or `ARRAY<INTERVAL>`, passed the call of
    `updateDatabaseDdl` and failed in the operation with code 12 and "Column type INTERVAL
    is not supported." (recorded: "a table with an INTERVAL column, a FLOAT32 column and an
    array of INTERVAL: poll 1 of the operation"). The same table has a `FLOAT32` column, so
    its mutation was not measured either.
  - A fraction of a nanosecond, and an `ARRAY<INTERVAL>` as an expression, are not
    measured.
- `ARRAY` has `arrayElementType` in the type, and the value is a JSON array with
  elements in the form of the element type. An empty array is `[]`. A NULL array is
  `null`, which differs from an empty array (recorded: "ARRAY of INT64", "an empty array
  and a NULL array"). An element can be `null` for every element type (recorded: "ARRAY
  of the other types"). A table of every element type round trips (recorded: "the rows
  of every type").
- `STRUCT` as a column of a result answers HTTP 501 with "A struct value cannot be
  returned as a column value" (recorded: "STRUCT as a column", "a NULL STRUCT as a
  column"). An `ARRAY` of `STRUCT` works, and each struct is an array of values in the
  order of its fields (recorded: "ARRAY of STRUCT"). A column of `SPANNER_SYS` that is
  an `ARRAY` of `STRUCT` with an `ARRAY` inside a struct also works, as nested arrays
  (recorded: "a SPANNER_SYS table"). A struct inside a struct of an array that a
  query builds answers HTTP 501 (recorded: "a nested STRUCT inside an ARRAY"). A
  `STRUCT` is a parameter (recorded: "a STRUCT parameter").
- A NULL is JSON `null` in every type (recorded: "a NULL of each type", "the rows of
  every type"). A NULL with no type has the type `INT64` (recorded: "a bare NULL").
- `TOKENLIST` is a type that a result cannot return. A statement that names a `TOKENLIST`
  column answers HTTP 400 `INVALID_ARGUMENT`: "TOKENLIST is an internal-only type and
  cannot be returned to the user by SQL query." (recorded: "a TOKENLIST column that the
  statement names"). The catalog shows the type as `TOKENLIST` (recorded: "the TOKENLIST
  column in the catalog"). So it is not a type of the table below.
- `ENUM`, `PROTO` and the types of the PostgreSQL dialect (`bigint`, `numeric`, `jsonb`
  and others) are not measured. The script has no descriptor bundle for `ENUM` and
  `PROTO`, and the database has the GoogleSQL dialect (source: the brief and
  "getDatabase"). The types of graph queries are not measured.
- The type names in a column of `INFORMATION_SCHEMA.COLUMNS.SPANNER_TYPE` carry the
  length, such as `STRING(MAX)`, `STRING(100)`, `BYTES(MAX)`, `ARRAY<INT64>` (recorded:
  "the columns").

The mapping is in the table below. The table was written by hand for step 8a. Step 10
will generate it from the code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| BOOL | boolean | `bool` | `bool` | `BOOL` | yes |
| INT64 | integer | `int64` | `int64` | `INT64` | yes |
| FLOAT32 | float | `float64` | `float64` | `FLOAT32` | yes |
| FLOAT64 | float | `float64` | `float64` | `FLOAT64` | yes |
| NUMERIC | decimal | `*apd.Decimal` | `*apd.Decimal` | `NUMERIC` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| BYTES | binary | `[]byte` | `[]uint8` | `BYTES` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIMESTAMP | timestamp | `time.Time` | `time.Time` | `TIMESTAMP` | yes |
| JSON | json | `the decoded JSON value` | `interface {}` | `JSON` | yes |
| UUID | uuid | `uuid.UUID` | `uuid.UUID` | `UUID` | yes |
| INTERVAL | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| ARRAY | array | `[]any` | `[]interface {}` | `ARRAY` | yes |
<!-- /dbimp:types -->

Notes on the mapping (decided, D191):

- `FLOAT32` has the kind float, and the kind has the Go type `float64`, as ClickHouse
  `Float32` does. The wire text is the widened `float32`, so `0.1` shows as
  `0.10000000149011612`. A caller that wants the short text of a `float32` converts the
  value back to a `float32`, and that conversion is exact. Both models said that
  `database/sql/driver` has no `float32`.
- `JSON` is a decoded value, as the kind says. The text `"null"` decodes to nil, so a
  JSON `null` and a SQL NULL look the same after the decode, and the number `12345678901234567890`
  needs `dbimp.Number` to stay exact (D19). Both models advised the text or a
  `json.Number`. This is the doubtful mapping.
- `INTERVAL` is ISO 8601 text with a sign on each part. `dbimp.Interval` holds months,
  days and nanoseconds. The forms of a whole part, of a fraction down to a microsecond
  and of a negative time are recorded, and the text maps on them. A nanosecond is not
  measured. Only an expression or a parameter has the type, because no column can.
- `ARRAY` is `[]any` of the Go types of its elements. A NULL array is nil, so the
  column can be NULL, which the array of BigQuery cannot.
- A `STRUCT` inside an `ARRAY` has the kind tuple and the Go type `[]any`. The names of
  its fields are in the column type only. The type table has no row for it, because a
  `STRUCT` as a column is refused.
- `TIMESTAMP` has nanoseconds, as `time.Time` does. The kind has no loss, and a time
  before 1970 keeps its nanoseconds.
- `NUMERIC` has a scale of 9 and 38 digits, so it fits an `*apd.Decimal` with no loss.
- The recorded types of a column have a length in the `INFORMATION_SCHEMA` text, and
  no length in the wire type.
- `TOKENLIST` has no row, because a result refuses it.

## Parameters

- The parameters are named, as `@name`. The body has `params`, an object of values,
  and `paramTypes`, an object of types, both keyed by the name with no `@` (recorded: "a
  parameter of every type").
- A positional `?` answers HTTP 400 "Positional parameters are not supported"
  (recorded: "a positional parameter"). The driver uses the parser of the root
  package to turn `?` into names (D34).
- A parameter that the statement uses and `params` lacks answers HTTP 400 "No parameter
  found for binding: p" (recorded: "a parameter that is missing").
- `paramTypes` is optional. A parameter with no type was accepted, and the server gave
  `p` the type `INT64` for the string `"7"` (recorded: "a parameter with no paramTypes").
  So the driver always sends the type.
- A value has the JSON form of its type, with the types above: an `INT64` is a string
  (recorded: "the parameter INT64 alone"), and a JSON number for an `INT64` answers
  HTTP 400 "Invalid value for bind parameter p: Expected INT64" (recorded: "an INT64
  parameter given as a JSON number"). A text that is not a number answers the same
  (recorded: "an INT64 parameter that is not a number").
- Every type works as a parameter: `INT64`, `FLOAT64`, `FLOAT32`, `NUMERIC`, `BOOL`, `STRING`,
  `BYTES`, `DATE`, `TIMESTAMP`, `JSON`, `UUID` and `INTERVAL` (recorded: "a parameter of
  every type", the "parameter ... alone" requests). A NULL of each type is `null` with
  its type (recorded: "a NULL of each type").
- An array is a JSON array with `paramTypes` `{"code":"ARRAY","arrayElementType":{...}}`.
  An empty array and a NULL array both work (recorded: "an array of each type", "an
  empty array and a NULL array"). `IN UNNEST(@ids)` works (recorded: "an array
  parameter in an IN UNNEST").
- A struct parameter is a JSON array in the order of its fields, with `structType` in
  `paramTypes` (recorded: "a STRUCT parameter", "a NULL STRUCT parameter", "an array of
  STRUCT parameters").
- A DML statement takes parameters the same way (recorded: "a DML statement with
  parameters"). `executeBatchDml` takes `params` and `paramTypes` in each statement
  (recorded: "executeBatchDml").
- A stream takes the same members (recorded: "a parameter on the stream").

## Transactions

- A DML statement needs a read write transaction. With no `transaction` it answers HTTP
  400 "DML statements can only be performed in a read-write transaction" (recorded: "a DML
  statement with no transaction"). In a single use read only transaction it answers HTTP
  400 "DML statements may not be performed in single-use transactions, to avoid replay"
  (recorded: "a DML statement in a read only transaction"). A single use read write
  transaction with `executeSql` answers HTTP 400 "Cannot use a single-use read-write
  transaction for ExecuteSql: use read-only instead" (recorded: "a single use read write
  transaction").
- A query with no `transaction` runs as a single use strong read (recorded: "a statement
  with no transaction"). `transaction.singleUse.readOnly` takes `strong`, `exactStaleness`,
  `maxStaleness`, `minReadTimestamp` and `returnReadTimestamp`, and a read with a
  staleness answers `metadata.transaction.readTimestamp` (recorded: "a statement with a
  single use read only transaction", "a single use read with exact staleness", "a single
  use read with maxStaleness", "a single use read with minReadTimestamp"). A single use
  read took `directedReadOptions` (recorded: "a single use read with
  directedReadOptions"). Whether any of them has an effect is not measured.
- `beginTransaction` with `options` answers `{"id": "{base64}"}`. The id is base64 text.
  - `readWrite` (recorded: "begin a read write transaction"). A statement then sends
    `transaction: {"id": ...}` and `seqno`, a string that grows for each statement
    (recorded: "an insert in the transaction", "an update in the transaction").
    A DML statement with no `seqno`, after four statements, answered HTTP 400
    `INVALID_ARGUMENT` "Request has an out-of-order seqno. Request seqno=0. Last seen
    seqno=4." (recorded: "a DML statement with no seqno"). A number can skip: the
    next statement sent `seqno` 6 after 4 and was accepted (recorded: "SELECT FOR
    UPDATE"). A DML statement with no `seqno` as the first statement is not measured.
    The queries that had no `seqno` in a read only transaction and as the only
    statement of a read write transaction worked (recorded: "a read in the read only
    transaction", "a read in that transaction").
  - `readWrite` took `readLockMode` `OPTIMISTIC`, and the options took
    `excludeTxnFromChangeStreams` (recorded: "begin a transaction with readLockMode and
    excludeTxnFromChangeStreams"). A read and a commit then worked. Their effect is not
    measured.
  - `readOnly` with `strong`, `exactStaleness` or `readTimestamp`, and
    `returnReadTimestamp`, which adds `readTimestamp` to the answer (recorded: "begin a
    strong read only transaction", "begin a read only transaction with exact
    staleness", "begin a read only transaction at a read timestamp"). A read at the read
    timestamp gave the count of that moment (recorded: "a read at the read timestamp").
  - `partitionedDml` (recorded: "begin a partitioned DML transaction").
  - A transaction id that is not valid answers HTTP 400 with a `BadRequest` on
    `transaction_id`, and an id that is not base64 answers HTTP 400 (recorded: "a
    transaction id that is not valid", "a transaction id that is not base64").
- A statement can begin the transaction: `transaction: {"begin": {"readWrite": {}}}`
  answers `metadata.transaction.id` (recorded: "a statement that begins a transaction
  inline", "load 5000 rows for a slow DDL"). It saves one round trip.
- `commit` takes `transactionId` and optionally `mutations`, `returnCommitStats` and
  `maxCommitDelay`. It answers `commitTimestamp` and `commitStats.mutationCount`, a
  string (recorded: "commit with the stats", "commit the DML forms with maxCommitDelay").
  The count follows the cells that were written: 5000 rows of three columns gave
  `"15000"` (recorded: "commit the 5000 rows"). A second commit of the same
  transaction answered HTTP 200 with the same timestamp (recorded: "commit again").
- `commit` of a read only transaction answers HTTP 400 `FAILED_PRECONDITION` "Cannot
  commit a read-only transaction" (recorded: "commit a read only transaction"). So the
  driver sends no commit for it.
- `commit` takes `singleUseTransaction` with `readWrite` and mutations in one call
  (recorded: "insert the rows of every type"). It accepted `isolationLevel`
  `REPEATABLE_READ` (recorded: "a commit with a single use transaction and the stats").
- The mutations are `insert`, `update`, `insertOrUpdate`, `replace` and `delete`, each
  with `table`, `columns` and `values`, and `delete` with a `keySet` of `keys` or `ranges`
  (recorded: "commit with every kind of mutation"). Values have the forms of the types. A
  table in a named schema takes its dotted name (recorded: "a mutation to a table of the
  named schema").
- `rollback` answers `{}` and the rows are not kept (recorded: "rollback", "the rows after
  the rollback").
- An `ABORTED` transaction answers HTTP 409 with `status` `ABORTED` and a `RetryInfo` detail
  with `retryDelay`, such as `"0.117856360s"` (recorded: "commit the older transaction").
  The older transaction was wounded by a younger one that locked the row first. A new
  transaction then succeeded (recorded: "commit the retry"). The conflict showed at the
  commit, and the statement of the older transaction answered HTTP 200 (recorded: "the
  older transaction updates the same row"). The retry rule belongs to the caller of
  `database/sql`, because a driver cannot replay a closure.
- `executeBatchDml` takes `statements`, each with `sql`, `params` and `paramTypes`, and
  `seqno`. It answers `resultSets`, each with `stats.rowCountExact`, and `status`. When a
  statement fails, the answer is HTTP 200, `resultSets` holds the results before it, and
  `status` holds the error, such as `{"code": 6, "message": "Row [111] ... already
  exists"}` (recorded: "executeBatchDml with a statement that fails"). The commit that
  followed answered HTTP 409 (recorded: "commit the batch"). It took `lastStatements`
  (recorded: "executeBatchDml with lastStatements").
- `lastStatement` on a DML statement was accepted. The next statement in that
  transaction answered HTTP 400 `FAILED_PRECONDITION` "Reads and queries (including DML)
  are not allowed after setting last statement option in the transaction", and the commit
  worked (recorded: "a DML statement with lastStatement", "a statement after
  lastStatement", "commit the transaction of lastStatement"). On a query it answers HTTP
  400 "may only be used with a request that includes a DML statement" (recorded: "a
  statement with lastStatement").
- `read` and `streamingRead` take `table`, `columns`, `keySet` (`all`, `keys` or
  `ranges` with `startClosed`, `endOpen` and similar) and `limit` (recorded: "a read with a
  key set of keys", "a read with a range", "a streaming read").
- A batch read only transaction gives partitions. `partitionQuery` with `maxPartitions` 2
  answered three partitions with a `partitionToken` of about 12,000 characters, and
  `partitionRead` answered three with tokens of about 210 characters (recorded:
  "partitionQuery", "partitionRead"). A statement with a token ran in that transaction
  and answered rows (recorded: "a statement with a partition token"). A token with no
  read only transaction answers HTTP 400 "Partitioned reads can only be performed in a
  read-only transaction" (recorded: "a statement with a partitionToken that is not
  valid").
- A read write transaction on the multiplexed session works. A DML statement answers a
  `precommitToken` with `precommitToken` and `seqNum`, and `commit` takes that token as
  `precommitToken` (recorded: "a DML statement on the multiplexed session", "commit on
  the multiplexed session with the precommit token"). A commit with no token, in a
  transaction with no statement, answered `precommitToken` and no `commitTimestamp`, so
  the commit must be sent again with that token (recorded: "commit on the multiplexed
  session with no precommit token"). The rule of the retry comes from the documentation
  of `CommitResponse` and is not measured.
- Sessions:
  - `createSession` answers `name`, `createTime` and `approximateLastUseTime`. A body
    `{"session": {"labels": {...}}}` sets labels, and `{"session": {"multiplexed": true}}`
    makes a multiplexed session, which has `multiplexed: true` and no
    `approximateLastUseTime` (recorded: "createSession with labels", "createSession
    multiplexed").
  - `batchCreateSessions` with `sessionCount` and `sessionTemplate` answers `session`, a
    list (recorded: "batchCreateSessions").
  - `getSession` shows no labels, though `listSessions` shows them (recorded: "getSession
    with labels", "listSessions with a filter"). `listSessions` takes `filter` and
    `pageSize` and does not list the multiplexed session (recorded: "listSessions at the
    end"). A session that no request used for 40 minutes was still listed (recorded:
    "listSessions at the end").
  - `deleteSession` answers `{}`, even for a second delete and for a session that is
    already gone (recorded: "deleteSession", "deleteSession a second time", "deleteSession
    of a session of the first run, deleted"). It answers HTTP 400 "Multiplexed sessions
    may not be deleted" for a multiplexed session (recorded: "deleteSession of the
    multiplexed session").
  - A session that was made and is gone answers HTTP 404 `NOT_FOUND` "Session not found:
    {name}", with a `ResourceInfo` of the type `google.spanner.v1.Session`, on
    `getSession`, `executeSql`, `executeStreamingSql` and `beginTransaction`. It did so
    at once, and 61 seconds later, and for the session that the first pass deleted
    (recorded: "getSession after the delete", "a statement on the deleted session", "a
    statement on the stream on the deleted session", "beginTransaction on the deleted
    session", "a statement on the deleted session after a minute", "a statement on a
    session of the first run, deleted"). The 404 of the stream has HTTP 404 and an array of
    one element.
  - The first pass got HTTP 200 for a statement on a session that it had deleted in the
    same second. The second pass got 404 for six such statements, for the same
    requests. The two differ in the age of the session, about 5 minutes and about 10
    minutes, and in nothing else that the recordings show. The cause of the 200 is not
    found, and it did not repeat. A driver must treat a 404 on a statement as a lost
    session, and must not rely on a 200 to show that a session is alive.
  - A name that is not a session answers HTTP 400 `INVALID_ARGUMENT`, with a `BadRequest`
    that says "Got: {name}" (recorded: "a session that does not exist", "a session of a
    database that does not exist"). A name that has the form of a session, with one
    changed character in the end or in the middle, answers the same 400 on `getSession`,
    `executeSql` and `deleteSession` (recorded: "getSession of a well formed session that
    was never made, with another end", "a statement on a well formed session that was
    never made, with another middle" and the four requests between). So the name carries a
    check, and a name that the server never made is a 400 and not a 404.
  - A session lasts about one hour with no use, and a statement on an expired session
    answers `NOT_FOUND` (source: the REST reference, not measured). The Go client uses a
    multiplexed session for every operation and renews it after seven days (read of
    `session.go` in `cloud.google.com/go/spanner` v1.95.1, 2026-10-10).

## Errors

- The body of an error is `{"error": {"code": N, "message": "...", "status": "NAME",
  "details": [...]}}`. `code` is the HTTP status. `status` is the name of the gRPC code.
  `details` holds typed objects such as `google.rpc.BadRequest` with `fieldViolations`,
  `google.rpc.ResourceInfo`, `google.rpc.ErrorInfo`, `google.rpc.LocalizedMessage` and
  `google.rpc.RetryInfo`.
- The statuses recorded:

| HTTP | status | Recorded |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | a syntax error, an unknown table, a wrong type, a positional parameter, an unknown member, a bad session name, a bad resume token, a bad API key, DDL or `MERGE` or `TRUNCATE` on `executeSql`, an out of order `seqno` ("a syntax error", "an unknown table", "a wrong type in a comparison", "a stream resumed with a token that is not valid", "getSession with a wrong key in the query", "MERGE", "a DML statement with no seqno") |
| 400 | `OUT_OF_RANGE` | a division by zero, a check constraint, `REPEAT` over 1 MB ("a division by zero", "a row that breaks the check constraint", "a result of one string of 11 MB") |
| 400 | `FAILED_PRECONDITION` | a foreign key, a commit of a read only transaction, a result of more than 10 MB on `executeSql`, a statement after `lastStatement` ("a foreign key to a missing parent", "commit a read only transaction", "big: twelve rows of 1 MB with executeSql", "a statement after lastStatement") |
| 403 | `PERMISSION_DENIED` | a call that the role lacks, and a database or an instance that does not exist ("getInstance", "createSession on a database that does not exist") |
| 404 | `NOT_FOUND` | a missing table or column in a mutation or a read, a missing row for an update, a session that is gone, an operation that does not exist ("a mutation to a missing table", "an update of a missing row", "getSession after the delete", "an operation that does not exist") |
| 404 | none (HTML page) | a path with no version, a method that the path lacks, an unresolved name ("a path with no version", "a statement with PUT") |
| 409 | `ALREADY_EXISTS` | a duplicate key, a unique index, an operation id that exists ("the same duplicate key", "the second use of a unique index", "the same operationId again") |
| 409 | `ABORTED` | a transaction that a conflict aborted, with `retryDelay` ("commit the older transaction") |
| 501 | `UNIMPLEMENTED` | two statements in one request, a struct as a column, `DELETE` of an operation ("two statements in one request", "STRUCT as a column", "delete a finished operation") |

- The text of `message` for a syntax or a name error holds a literal backslash and the
  letter `n` where a line break belongs, and `\"` for a quote. The member
  `details[].message` of the type `LocalizedMessage` holds the real line break (recorded:
  "a syntax error", "an unknown table"). A driver that shows the message must use the
  detail, or turn the two characters into a line break.
- An error with HTTP 200: `executeBatchDml` answers `status` in the body (recorded:
  "executeBatchDml with a statement that fails"). A stream that fails after some rows
  answers HTTP 200 and ends with an element that holds `error` (recorded: "a division by
  zero in row 3500 of 4000, unsorted, on the stream"). A long running operation of the DDL
  answers `done: true` and an `error` with a `code` and a `message`. The recorded codes
  are 5 for a missing table, view or change stream, 12 for a column type that is not
  supported and 1 for a cancel (recorded: "a DDL call that drops a table that does not
  exist: poll 1 of the operation", "a table with an INTERVAL column, a FLOAT32 column and
  an array of INTERVAL: poll 1 of the operation", "the running operation after a wait").
  The `code` of an operation error is the gRPC number, not the HTTP status. A DDL call
  that the server refuses at once answers HTTP 400 (recorded: "a DDL call with a syntax
  error").
- An error on a stream: an error before any row has HTTP 400 and an array with one
  object that has `error` (recorded: "a division by zero after 5000 rows on the stream",
  "a stream resumed with a token that is not valid"). A stream on a session that is gone
  answers HTTP 404 in the same form (recorded: "a statement on the stream on the deleted
  session"). An error after rows has HTTP 200 and the object that has `error` is the
  last element (recorded: "a division by zero in row 3500 of 4000, unsorted, on the
  stream").
- A constraint error shows at the commit or at the DML statement, not at the call that
  built the rows (recorded: "a duplicate key", "the same duplicate key").
- The limit of the rate (HTTP 429), HTTP 401, 500, 503 and 504 are not measured.
  Gemini named 429, 503 and 504. DeepSeek named 401, 429, 500, 503, 504 and the retry of
  `UNAVAILABLE`, `RESOURCE_EXHAUSTED` and `DEADLINE_EXCEEDED` (not measured). The Go client retries on
  those codes and uses `RetryInfo` (read of `retry.go` in `cloud.google.com/go/spanner`
  v1.95.1, 2026-10-10).
- Which errors mean that the request did not reach the server: a network error before
  the answer, such as a refused connection or a TLS failure. No recording shows one.
  The answer HTTP 400 `INVALID_ARGUMENT` for a bad session is not one of them.
- A wrong token: the server answered HTTP 200 for the token with `-wrong` at the end. The
  answer to a token that the server refuses is not measured. See Requests.

## Cancellation and timeouts

- The recorder cannot cancel a request, and a client timeout is not recorded (source:
  the comments of the generator `gen.py` of the main session). So what the server does
  when the client disconnects is not measured.
- A query has no cancel call on the REST API (source: the REST reference, which lists
  none, and the main session, not measured).
- `POST {database}/operations/{id}:cancel` cancels an operation of the DDL. The body is
  `{}` and the answer is HTTP 200 `{}` (recorded: "cancel the running operation").
  - The operation was three indexes on a table of 5000 rows. The cancel came 2 seconds
    after the call. A read of the operation 2 seconds later was not done and showed 1
    percent (recorded: "the running operation just after the cancel"). A read 21 seconds
    later was `done: true` with an `error` of code 1 and "Statement was cancelled by user
    request.", and the `endTime` was about 8 seconds after the start (recorded: "the
    running operation after a wait"). None of the three indexes exists afterwards
    (recorded: "the indexes").
  - A cancel of a finished operation answered HTTP 200 `{}` and changed nothing: the
    operation stayed done with its `response` (recorded: "cancel a finished operation",
    "the operation that was cancelled").
  - A cancel and a read of an operation that does not exist answer HTTP 404 `NOT_FOUND`
    "Operation not found" with a `ResourceInfo` (recorded: "cancel an operation that does
    not exist", "an operation that does not exist"). A `DELETE` of an operation answers
    HTTP 501 and changes nothing (recorded: "delete a finished operation", "the
    operation after the delete").
- A DDL call returns at once, and the operation runs on the server. A client that stops
  waiting leaves the operation to run (recorded: "a DDL call with an operationId": the
  operation was done 3 seconds after its start). A cancel of a DDL operation does not
  undo a statement that finished.
- The deadline of a request: not measured. A request can send `x-goog-request-params` and
  other headers for routing, which the Go client sets (source: Gemini, not measured here).

## Statements

- Two statements in one request answer HTTP 501 "SQL queries are limited to single
  statements" (recorded: "two statements in one request"). A trailing semicolon is
  accepted (recorded: "a trailing semicolon"). A line comment and a block comment
  before the statement are accepted (recorded: "a line comment", "a block comment").
- A DDL statement does not go to `executeSql`. It answers HTTP 400 `INVALID_ARGUMENT`
  "DDL statements cannot be processed by the Query API. Please use DDL API or DDL UI
  instead.", and the table is not made (recorded: "a CREATE TABLE statement sent to
  executeSql", "a CREATE TABLE statement sent to executeStreamingSql", "the tables that
  the two statements made"). `updateDatabaseDdl` takes `statements`, an array of DDL
  text, and an optional `operationId`. One call can hold many statements (recorded:
  "setup: create the core tables").
  - The answer is a long running operation: `name`, and `metadata` of the type
    `UpdateDatabaseDdlMetadata` with `database`, `statements`, `progress` and
    `actions` (recorded: "add a column"). The server rewrites the statements in a
    canonical form, with line breaks and `PRIMARY KEY(id)`. `getDatabaseDdl` shows
    them in that form, sorted by table name, and shows a foreign key as an `ALTER TABLE
    ... ADD CONSTRAINT` (recorded: "getDatabaseDdl after the changes"). It answers `{}`
    when the database has no object (recorded: "teardown: the database after the
    drops").
  - `GET` of the operation answers the same with `done: true`, `commitTimestamps`,
    `progress` of 100 and `response` of the type `google.protobuf.Empty` (recorded: "add a
    column: poll 1 of the operation"). A member `done` that is false is missing from the
    answer, and `progress` shows `progressPercent`.
  - Each DDL call has one `GET` of the operation in the recordings, sent about 16 to 17
    seconds after the call, and most operations were done by then. They took about 3
    seconds, and a change stream took 7 seconds (recorded: "a change stream: poll 1 of
    the operation"). Two were not done at the first `GET`. "indexes: unique, null filtered
    and storing" showed 87 percent, and the index showed `READ_WRITE` in
    `INFORMATION_SCHEMA.INDEXES` later (recorded: "the indexes"). "a search index" showed 40
    percent, and a second `GET` 17 seconds later was done, with 29 seconds from start to end
    (recorded: "a search index: poll 1 of the operation", "a search index: poll 2 of the
    operation"). The teardown of the tables was done at its first `GET` (recorded:
    "teardown: drop the indexes, the tables and the sequence: poll 1 of the operation").
  - A DDL call that was not polled, so its result is not measured: the drops of the view,
    the change stream, the search index, the table of the named schema and the named schema
    in the setup and the teardown, and "drop the view" and "drop the change stream" of the
    middle of the script. The later queries show that the drops worked (recorded: "the view
    after the drop", "teardown: the database after the drops").
  - An `operationId` is accepted and names the operation (recorded: "a DDL call with an
    operationId"). The same id again answers HTTP 409 `ALREADY_EXISTS` (recorded: "the
    same operationId again").
  - A syntax error is refused at the call with HTTP 400 and the line and the column
    (recorded: "a DDL call with a syntax error").
  - A drop of an object that does not exist passes the call and fails in the operation
    with code 5, for `DROP TABLE`, `DROP VIEW` and `DROP CHANGE STREAM`. The forms
    with `IF EXISTS` are done with no error (recorded: "a DDL call that drops a table that
    does not exist: poll 1 of the operation", "DROP VIEW of a view that does not exist:
    poll 1 of the operation", "DROP VIEW IF EXISTS of a view that does not exist: poll 1 of
    the operation", "DROP CHANGE STREAM of a change stream that does not exist: poll 1 of
    the operation", "DROP CHANGE STREAM IF EXISTS of a change stream that does not exist:
    poll 1 of the operation").
- The DDL that the script ran and the server accepted, with its operation done and with no
  error unless the text says so:
  - `CREATE TABLE` with every type, with `INTERLEAVE IN PARENT ... ON DELETE CASCADE`, with a
    foreign key, a check, a stored generated column and a `DEFAULT` expression.
  - `ALTER TABLE ADD COLUMN` and `DROP COLUMN`, and a column with `allow_commit_timestamp`
    (recorded: "a column that takes the commit timestamp").
  - `CREATE UNIQUE INDEX`, `CREATE NULL_FILTERED INDEX ... STORING` and an index with a
    `DESC` key. A foreign key made its own backing index (recorded: "the indexes").
  - `CREATE SEQUENCE` with `bit_reversed_positive` and a column that uses
    `GET_NEXT_SEQUENCE_VALUE`.
  - `CREATE CHANGE STREAM` and the drop of each object.
  - Views, search indexes, row deletion policies and named schemas, which the next
    items describe.
- Views. `CREATE VIEW dbimp_t_v SQL SECURITY INVOKER AS SELECT t.id AS id, t.s AS s FROM
  dbimp_t_dml t` was done with no error (recorded: "a view: poll 1 of the operation"). A
  query on the view answered rows, and `INFORMATION_SCHEMA.VIEWS` holds the definition and
  `security_type` `INVOKER` (recorded: "the view", "the views"). After `DROP VIEW` the query
  answers HTTP 400 "Table not found: dbimp_t_v" (recorded: "the view after the drop"). The
  first pass sent the same view with no table alias, `SELECT id, s FROM dbimp_t_dml`, and the
  operation failed with "Alias id cannot be used without a qualifier in strict name
  resolution mode". The second pass did not send that form again.
- A search index. A table with a `TOKENLIST` column,
  `body_tokens TOKENLIST AS (TOKENIZE_FULLTEXT(body)) HIDDEN`, and `CREATE SEARCH INDEX` on
  it were accepted (recorded: "a table with a TOKENLIST column", "a search index"). The
  predicate `SEARCH(body_tokens, 'hello')` answered the one row that has the word
  (recorded: "a search of the index"). `INFORMATION_SCHEMA.INDEXES` shows the index with
  `index_type` `SEARCH` (recorded: "the search index in the catalog"). A statement that
  names the `TOKENLIST` column is refused (see Types).
- A row deletion policy. `CREATE TABLE ... ROW DELETION POLICY (OLDER_THAN(ts, INTERVAL 30
  DAY))` was accepted, and `INFORMATION_SCHEMA.TABLES.row_deletion_policy_expression` holds
  `OLDER_THAN(ts, INTERVAL 30 DAY)` (recorded: "a table with a row deletion policy", "the row
  deletion policy"). A row of the year 2000 was still there after the insert. When the server
  deletes it is not measured.
- A named schema. `CREATE SCHEMA dbimp_t_sch` and `CREATE TABLE dbimp_t_sch.t1` were
  accepted. A mutation, a DML statement, a `SELECT` and the catalog all take the dotted name
  (recorded: "a named schema", "a table in the named schema", "a mutation to a table of the
  named schema", "an insert into a table of the named schema", "a table of the named schema
  by its name", "the tables of the named schema").
- DML: `INSERT`, `UPDATE` and `DELETE` with `THEN RETURN` and with parameters work in a
  read write transaction (recorded: "an insert with THEN RETURN", "a delete with THEN
  RETURN"). A DML statement that changes no row answers 0.
  - `INSERT OR UPDATE` and `INSERT OR IGNORE` work as statements. Each answered
    `rowCountExact` 1, except `INSERT OR IGNORE` of a row that exists, which answered 0
    (recorded: "INSERT OR UPDATE of a new row", "INSERT OR UPDATE of a row that exists",
    "INSERT OR IGNORE of a row that exists", "INSERT OR IGNORE of a new row").
  - `MERGE` answers HTTP 400 `INVALID_ARGUMENT` "Statement not supported: MergeStatement"
    (recorded: "MERGE").
  - `TRUNCATE TABLE` answers HTTP 400 "Statement not supported: TruncateStatement" on
    `executeSql` (recorded: "a TRUNCATE statement"). In the DDL call it answers HTTP 400
    with a syntax error at `TRUNCATE` (recorded: "a TRUNCATE TABLE statement in the DDL
    API"). The 5000 rows stayed (recorded: "the rows of the table after the TRUNCATE
    TABLE"). So Spanner has no `TRUNCATE`. No source named it, so `features.json` has
    no entry for it.
  - A column with a sequence default takes its value from the sequence. `INSERT ... THEN
    RETURN id` answered an `INT64` such as `"7991637538768945152"`, and
    `GET_NEXT_SEQUENCE_VALUE(SEQUENCE dbimp_t_seq)` answered one in a read write
    transaction (recorded: "an insert that takes its key from a sequence, with THEN
    RETURN", "the next value of a sequence in a query").
  - A DML statement can use `PENDING_COMMIT_TIMESTAMP()` (recorded: "a DML statement with
    the pending commit timestamp").
- Hints. A table hint `@{FORCE_INDEX=dbimp_t_idx_nf}` and a statement hint
  `@{USE_ADDITIONAL_PARALLELISM=TRUE}` work in a query (recorded: "a hint that forces an
  index", "a statement hint"). A name of an index that does not exist answers HTTP 400
  "The table dbimp_t_dml does not have an index called dbimp_t_nosuch_idx" (recorded: "a
  hint that names an index that does not exist"). The statement hint in a DML statement
  answers HTTP 400 "Unsupported hint: USE_ADDITIONAL_PARALLELISM." (recorded: "a DML
  statement with a hint").
- `SELECT ... FOR UPDATE` works in a read write transaction and answers the row (recorded:
  "SELECT FOR UPDATE"). With no transaction it answers HTTP 400 "Unsupported lock mode: FOR
  UPDATE is not supported in this transaction type." (recorded: "FOR UPDATE with no
  transaction").
- The messages of a constraint give the key, such as "Row [1] in table dbimp_t_dml already
  exists" (recorded: "the same duplicate key"). A row that a foreign key references cannot
  be deleted (recorded: "delete the parent and its interleaved children"). The same delete
  works after the referencing row is gone, and it deletes the interleaved children (recorded:
  "delete the parent after the reference is gone, with its interleaved children").
- Graph queries are not measured.

## Principals

The one principal is a service account with `roles/spanner.databaseAdmin` on the database
`dbimp_test`. The manifest has `noOrdinaryUser`.

- The database calls work: sessions, statements, DDL, `getDatabase`, `getDatabaseDdl`,
  `listDatabaseRoles` and the list of operations.
- The calls at the level of the instance answer HTTP 403 `PERMISSION_DENIED`, with
  `ErrorInfo` that names the missing permission (recorded: "getInstance", "listInstances",
  "listInstanceConfigs", "listDatabases"). The permissions were `spanner.instances.get`,
  `spanner.instances.list`, `spanner.instanceConfigs.list` and `spanner.databases.list`.
- A session on a database that does not exist, and on an instance that does not exist,
  answers HTTP 403 and names `spanner.sessions.create`, not 404 (recorded: "createSession
  on a database that does not exist", "createSession on an instance that does not
  exist"). The error does not show that the name is wrong.
- The roles of the database are `public`, `spanner_info_reader` and `spanner_sys_reader`
  (recorded: "listDatabaseRoles").
- Version and metadata:
  - `getDatabase` answers `state`, `createTime`, `versionRetentionPeriod` of `1h`,
    `earliestVersionTime`, `encryptionInfo` and `databaseDialect` (recorded: "getDatabase").
  - The version that `usql` runs: none. `usql/drivers/spanner/spanner.go` registers
    `drivers.Driver{}` with no `Version` function (read 2026-10-10), so `usql` sends no
    statement for the version of this product. `dbmeta` runs the query of the next item. As the
    one login it got `"9"` (recorded: "the greatest optimizer version"). The manifest has no
    ordinary user, so the answer for another user is not measured. The driver answers no
    `SELECT version()` itself, because D181 allows that only for a product that has no query
    for its release, and `SPANNER_SYS` has one (D191 item 11).
  - There is no version function (recorded: "a version function"). `dbmeta` reads the
    highest optimizer version from `SPANNER_SYS.SUPPORTED_OPTIMIZER_VERSIONS` (source:
    `dbmeta/models/spanner/spanner.go`). The table has nine rows, versions 1 to 9, with a
    `RELEASE_DATE` and `IS_DEFAULT`, and version 9 is the default (recorded: "the supported
    optimizer versions"). The query of `dbmeta` answered `"9"` (recorded: "the greatest
    optimizer version").
  - `INFORMATION_SCHEMA` answered `SCHEMATA` (the schemas are `""`, `INFORMATION_SCHEMA` and
    `SPANNER_SYS`), `DATABASE_OPTIONS`, `TABLES` (with `parent_table_name`,
    `on_delete_action` and `row_deletion_policy_expression`), `COLUMNS` (with
    `spanner_type`, `is_generated` and `column_default`), `INDEXES`, `INDEX_COLUMNS`,
    `TABLE_CONSTRAINTS`, `CHECK_CONSTRAINTS`, `VIEWS`, `SEQUENCES` and `CHANGE_STREAMS`
    (recorded: "the schemata", "the options of the database", "the tables", "the
    columns", "the indexes", "the index columns", "the constraints", "the check
    constraints", "the views", "the sequences", "the change streams").
  - A table that the script did not name, such as `DATABASE_OPTIONS`, has its column names
    in upper case (recorded: "the options of the database"). A table of `SPANNER_SYS`
    answered with one row of 36 columns in upper case, which include `ARRAY` and
    `ARRAY` of `STRUCT` columns (recorded: "a SPANNER_SYS table").
  - `INFORMATION_SCHEMA.TABLES` shows the interleave and `CASCADE` (recorded: "the
    tables").

## Flavors

- Spanner has two dialects, `GOOGLE_STANDARD_SQL` and `POSTGRESQL`. The database
  recorded has the first (recorded: "getDatabase"). The second is not measured, and its
  types and its parameters (`$1`) differ (source: the documentation, not measured).
- Spanner Omni and the Cloud Spanner emulator speak gRPC, and the emulator also serves
  REST. See "Measured on Omni". Whether a driver can tell the hosted service from the
  emulator is not measured. The Go client has settings for an experimental host (source:
  `go-sql-spanner` `isExperimentalHost`, read 2026-10-10).

## Interfaces

`spanner/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, the token that the driver gets with the key file of the DSN, and the multiplexed session of each database (D191). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. A multiplexed session cannot be deleted, so the server ends it. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the token, the session and the login, and costs little. |
| `driver.SessionResetter` | no | A connection holds a transaction only, and database/sql ends it before it reuses the connection (D102). The session is the connector's, and it holds no state. |
| `driver.Validator` | no | A connection holds nothing on the server that can go bad. A session that the server loses is dropped by the connector, and the statement that found it returns driver.ErrBadConn. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver binds with a type of its own: a decimal, a dbimp.Date, a dbimp.Interval, a UUID, a map for JSON, and a slice for an ARRAY (D191). |
| `driver.QueryerContext` | yes | The statement goes to executeStreamingSql with its arguments as named parameters with their types (D191). |
| `driver.ExecerContext` | yes | Exec reads the result to its end with no decoding, and RowsAffected is rowCountExact, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx calls beginTransaction, for a read-write or a read-only transaction. An ABORTED answer returns ErrAborted (D191). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, because the server refuses several (recorded: "two statements in one request"). |
| `driver.RowsColumnTypeScanType` | yes | The metadata names the type of each column, and each type has one Go type (D135 and D191). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The code of the type in upper case, such as INT64. An array is ARRAY. |
| `driver.RowsColumnTypeLength` | no | The wire type carries no length, because the length of STRING(100) is in the catalog and not in the metadata. |
| `driver.RowsColumnTypeNullable` | yes | The metadata names no nullability, so every column can be NULL (D18). |
| `driver.RowsColumnTypePrecisionScale` | yes | A NUMERIC column has a precision of 38 and a scale of 9. |
<!-- /dbimp:interfaces -->

## Faults

The driver of `usql` is `go-sql-spanner` v1.26.0. Read of `rows.go`, 2026-10-10:

- A SQL NULL of a `JSON` column is a `spanner.NullJSON` with `Valid` false, not nil. Hard
  rule 3 forbids it.
- A `DATE` is a `string` and a `UUID` is a `string`. A `NUMERIC` is a `*big.Rat`, or a
  `string` with a property. A `FLOAT32` is a `float32`. D135 gives the types of the kinds
  instead.
- An `ARRAY` has the element type `[]spanner.NullInt64` and similar by default, or `[]int64`
  with a property. An `ARRAY` of `STRUCT` or `INTERVAL` answers "unsupported array element
  type", and an `INTERVAL` column answers "unsupported type" (read of the `default` branch).
- The driver depends on gRPC, on the Google Cloud SDK and on the protos of Envoy
  (source: `usql/docs/BACKLOG.md`).
- The Go client has no session pool and uses a multiplexed session for every operation
  (read of `client.go` in `cloud.google.com/go/spanner` v1.95.1, 2026-10-10).

## Second opinions

- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) was asked the four questions of step 5a. It
  named `INSERT`, `UPDATE`, `DELETE`, `THEN RETURN`, mutations, partitioned DML and batch
  DML. It named the schema operations, `TTL`, named schemas, graph and `TOKENLIST`. The server
  showed the DML and the mutations (recorded: "an insert with THEN RETURN", "commit with
  every kind of mutation"), the row deletion policy and the named schema (see Statements),
  and it refused a `TOKENLIST` column in a result (see Types). Gemini said that a `STRUCT`
  is a JSON array of field values, and the server showed it for an `ARRAY` of `STRUCT` and
  refused it as a column (recorded: "STRUCT as a column"). It said that a `STRING` holds up
  to 10 MB and a `JSON` up to 8 MB, which is not measured.
- DeepSeek (`deepseek-v4-flash`, 2026-10-10) was asked the same four questions. Five
  tries of `deepseek-v4-flash` and `deepseek-v4-pro` gave no text, because the reasoning
  used the budget or the request timed out. A try with a short brief and a system prompt
  that asked for no long reasoning answered. It listed the DML, the mutations and the reads. It said
  that a `STRUCT` is a JSON object, which the server showed to be wrong (recorded: "ARRAY of
  STRUCT"). It said that an `ARRAY` holds at most 10,000 elements, which the server did not
  refuse for 5000 and is not measured above that. It named `FLOAT64` as a JSON number, which
  is true for a finite value and false for `NaN` (recorded: "FLOAT64 NaN and infinity"). It
  did not name `FLOAT32`, `UUID` and `INTERVAL`, which the server has (recorded: "the rows of
  every type", "UUID values", "INTERVAL values"). `qwen-max`, `qwen`, `kimi-k3` and
  `kimi` timed out on every try on 2026-10-10.
- The Go client `cloud.google.com/go/spanner` v1.95.1 and the driver
  `github.com/googleapis/go-sql-spanner` v1.26.0 were read on 2026-10-10 in the module
  cache. The Python client and the JDBC driver were not read, so the survey has no client in
  another language, and the REST reference stands in for it.
- Gemini and DeepSeek (2026-10-10) were asked what step 6 did not find. Their leads, and what
  the recordings of the second pass say:
  - `chunkedValue` must be joined. Measured: the server splits a value of more than about 1
    MiB, and the join is the text of the pieces in order. A `BYTES` value must be joined
    before the base64 decode (recorded: "big: a string of 3 MB on the stream", "big: a BYTES
    value of 2 MB from a table on the stream").
  - A `resumeToken` restarts a stream. Measured: the resumed stream holds the rest and has
    no `metadata` (recorded: "a stream resumed with the token"). A token that is wrong
    answers HTTP 400.
  - `metadata.transaction.id` comes in the first message of a stream that began a
    transaction. Measured for `executeSql` only (recorded: "a statement that begins a
    transaction inline").
  - The retry of `ABORTED` uses `retryDelay` of `RetryInfo`. Measured (recorded: "commit the
    older transaction").
  - A session lives about one hour, and the driver must keep it alive. Not measured. A
    session with no use for 40 minutes was still listed.
  - A commit has a limit of 100 MB and 20,000 mutations (Gemini), and a batch has 100
    statements (DeepSeek). Not measured. A commit of 15,000 mutations passed. The mutation
    count of a commit is in `commitStats` (recorded: "commit with the stats", "commit the
    5000 rows").
  - `maxCommitDelay`, `directedReadOptions`, `excludeTxnFromChangeStreams`,
    `readLockMode`, hints and `SELECT FOR UPDATE` were all accepted, and the hints and
    `SELECT FOR UPDATE` work as the previous sections say (recorded: "commit the DML forms
    with maxCommitDelay", "a single use read with directedReadOptions", "begin a transaction
    with readLockMode and excludeTxnFromChangeStreams", "a statement hint", "SELECT FOR
    UPDATE"). The effect of the options is not measured. `dataBoostEnabled` was not sent.
    `isolationLevel` `REPEATABLE_READ` was accepted on a commit (recorded: "a commit with a
    single use transaction and the stats").
  - `requestOptions` `priority` and `requestTag`, and `queryOptions`, were accepted
    (recorded: "requestOptions with a priority and a tag", "queryOptions"). Whether they have
    an effect is not measured.
  - `minReadTimestamp` and `maxStaleness` for a single use read only transaction were
    accepted and answered a read timestamp (recorded: "a single use read with maxStaleness",
    "a single use read with minReadTimestamp"). `exactStaleness` and `readTimestamp` were
    measured too.
  - `x-goog-request-params` and the headers of routing. Not measured, and the recordings
    needed none.
  - HTTP 401, 429, 503 and 504, and the backoff. Not measured.
  - A request in gzip. Not measured (see Requests).
  - A deleted session answers 404. Measured on every call of the second pass. The 200 of the
    first pass did not repeat (see Transactions).
- The review of the type mapping (step 8a), 2026-10-10:
  - Gemini said that `JSON` as a decoded value is doubtful, because a caller scans a `JSON`
    column into a struct and must encode it again. It advised `string` or `[]byte`. It said
    that `FLOAT32` is `float32` by the rule of the types, and `float64` by the rule of
    `driver.Value`, and that a decoded `JSON` needs `UseNumber` so that big numbers stay
    exact. It said that an `INT64` in an `ARRAY` stays `int64`, and a number inside a `JSON`
    does not become `int64`.
  - DeepSeek said that the other types are correct, and that a `JSON` `null` and a SQL
    NULL collide when the value is decoded to `any`. It advised the text, a wrapper or
    `json.Number`. It said that `FLOAT64` must read `"NaN"`, `"Infinity"` and `"-Infinity"`
    and keep `-0`. It said that `FLOAT32` is `float64` at the boundary of `driver.Value`.
    It said that `[]any`, `*apd.Decimal`, `dbimp.Date`, `dbimp.Interval` and `uuid.UUID` are
    not standard `driver.Value` types and need a `Scanner`, which D135 already decided.
  - The server showed that the `JSON` text `"null"` and a SQL NULL are different on the
    wire (recorded: "JSON values"), so the collision comes from the decode and not from the
    server. The two models of two vendors agree on `JSON` and `FLOAT32`, so both are in
    Open questions.

## Open questions

Ken decided the proposals of step 9 on 2026-10-10 (decided, D191). The list below holds
each one with its answer.

1. R and the tests: decided, D191. Development uses the hosted instance. The integration
   tests run on the Cloud Spanner emulator when `dbmeta` has its entry (D187), and on the
   hosted instance only where a person supplies it.
2. Reading a result: decided, D191. The driver reads every result with `executeStreamingSql`
   and joins the pieces of a `chunkedValue` as plain text before it decodes base64.
3. An error after rows: decided, D191. A broken stream returns its error. The driver reads
   each element and takes an element with `error` as the end of the result.
4. A resume: decided, D191. The driver does not resume a stream with the `resumeToken`.
5. The session: decided, D191. One multiplexed session for each connector, and a new one
   when the server answers `NOT_FOUND`.
6. Transactions: decided, D191. `BeginTx` calls `beginTransaction`. An `ABORTED` answer goes
   to the caller, and a commit of a read only transaction sends nothing.
7. Parameters: decided, D191, with one exception from D195 item 2. The driver turns `?`
   into `@p1`, `@p2` and sends `paramTypes`, except that a nil argument has no type,
   because the server infers it. `TestIntegrationNullParameters` checks a NULL for each type,
   and the driver sends `STRING` for a NULL that the server refuses. A caller can write
   `@name`. A statement that mixes both is an error.
8. Several statements: decided, D191. The driver refuses them, as the server does.
9. DDL: decided, D191. The driver sends a DDL statement to `updateDatabaseDdl` and polls
   the operation until it is done. When the context ends, it calls `operations:cancel`.
10. The credentials: decided, D191. A path to a key file in a key of the DSN query. The
    driver signs the JWT. A caller can pass an access token through a connector.
11. The doubtful types: decided, D191. `JSON` is a decoded value, `FLOAT32` is a `float64`
    and `INTERVAL` is a `dbimp.Interval`. The table above agrees.
12. `ENUM`, `PROTO`, `TOKENLIST` and the PostgreSQL dialect: decided, D191. They are not
    covered in the first version. `ENUM` and `PROTO` return an error that names the column,
    a `TOKENLIST` column cannot be selected, and the driver refuses a PostgreSQL database
    when it connects.
13. The matrix in `TYPES.md`: the type table above has its column there, and
    `TestTheTypeMatrixIsCurrent` holds that it is current.
14. The tests that `features.json` names exist in `spanner/`, and
    `TestEveryDriverHasItsFeatures` holds that. See "Integration tests".
15. The choices of the package: decided, D191 item 12 and D195. They are the keys of
    the DSN, `WithTimeout`, a lost session as `driver.ErrBadConn`, the mappings of the
    parameters, and the isolation levels. A DML statement outside a transaction commits
    when its rows end or close, also on an early close of `THEN RETURN` rows (D195 item
    3). Rule 4 of `AGENTS.md` names the Spanner transaction and the rows of such a
    statement (D194).
16. DML on the stream: not measured. The driver sends DML to `executeStreamingSql`, as D191
    item 11 says for every result. The recordings sent DML to `executeSql` only. The
    reference of `PartialResultSet` says that `stats` carries the count of a DML statement.
    `TestIntegrationDMLCount` checks the count live. If the stream gives none, DML goes to
    `executeSql`.

### Leads for a third run

These are facts that the recordings of the second pass do not settle. Each needs one request
with a valid name or a second value.

- A resume from the token of a later message of a stream, and a stream with an error after
  rows that has a `resumeToken` (the message before the error had none).
- A chunked list, and the limit of a stream above 12 MB. The exact edge of the limit of
  `executeSql`, between 3 MB and 12 MB, and a column value of 10 MB.
- A token that is not a token, an expired token, a token with the wrong scope and a request
  with no header. The `-wrong` token was accepted.
- A deleted session that answers 200, as in the first pass. Delete a session that is about 5
  minutes old and send a statement in the same second.
- A session with no use for more than an hour.
- A DML statement with no `seqno` as the first statement of a transaction, and an inline
  `begin` on `executeStreamingSql`.
- An `INTERVAL` of a nanosecond, and an `ARRAY<INTERVAL>` as an expression.
- `ENUM`, `PROTO` and a database of the PostgreSQL dialect. `dataBoostEnabled`.
- A vector index. The time that a row deletion policy takes to delete a row.
- A body in gzip with a real gzip stream.
- A 429, a 503 and a 504, if the service gives one.

## Integration tests

The integration tests of the driver read `SPANNER_DSN` and skip when it is empty (hard
rule 9). The variable holds the DSN of D191 with the path to the key file of the service
account that `dbsetup` made:

    SPANNER_DSN='spanner:///PROJECT/INSTANCE/DATABASE?credential_file=/path/to/key.json'

The tests also run on the Cloud Spanner emulator, when `dbmeta` has its entry (D187 and
D191 item 2). The DSN of the emulator is `spanner://localhost:9020/PROJECT/INSTANCE/DATABASE`,
and it needs no key file. `dbrun` starts no hosted service, and the workflow has no job
and no secret for it. A person runs the tests with the login that `dbsetup` made:

    go test -race -count=1 -run Integration -v ./spanner/...

The login is one service account with `roles/spanner.databaseAdmin` on one database and no
role on the instance, so each test runs as that account only, and the manifest has no
ordinary user. The tests make their tables, indexes, views, sequences, change streams and
schemas in the database of the DSN, with the name of the run as the prefix of each one
(`dbimp_it_` and eight characters). Each test drops what it makes, even when it fails.
`TestMain` looks for an object of the run that a test left, drops it and fails. A DDL
statement takes seconds on the hosted service, so the tests run one after the other, and
the whole run takes some minutes.

The tests were written on 2026-10-10 with no credential, and they have not run against the
service yet. The tests and what each one holds:

- `TestIntegrationConnect`: the ping, the type of each column, and several connections that
  share one session.
- `TestIntegrationErrors`: a syntax error, a missing table, a duplicate key, and an error
  after some rows.
- `TestIntegrationContext`: a deadline that ends while a query runs, and while a DDL
  statement runs.
- `TestIntegrationNullParameters`: a NULL with no type for each column type and for a nil slice,
  in a `SELECT` and in an `INSERT` (D195 item 2).
- `TestIntegrationIsolationLevels`: a transaction at each level that the driver allows.
- `TestIntegrationDMLCount`: the count of a DML statement outside and inside a transaction.
- `TestIntegrationTransactionRetry`: four transactions that add one to a row, and a caller
  that runs a transaction again when the error wraps `ErrAborted`.
- `TestIntegrationCRUD`, `TestIntegrationSchema` and `TestIntegrationFeatures`: the entries of
  `features.json`, in the order that the file names them. The driver sends SQL only, so a
  mutation, a read by key, a partitioned statement, a batch, a partition and a session call
  are not reachable, and their subtests skip with that reason. A feature that is a member of a
  request, such as `queryMode` or `directedReadOptions`, goes through `WithParameter`.
- `TestIntegrationRoundTrip`: every type that `features.json` marks yes, with
  `dbimptest.RoundTrip`, as a bound argument and as a literal. A column cannot have the type
  `INTERVAL`, so its values go in as the text of a `STRING` column that the statements turn into
  an `INTERVAL`. The two types that the survey marks no, `STRUCT` and `TOKENLIST`, have a test of
  their refusal.

The replay tests run with no credential. They answer from the recorded exchanges, and
`TestReplayEveryType` reads the exchange "the rows of every type on the stream" through the
real decoder (step 14a). The recorder sent most statements to `executeSql`, and the driver sends
`executeStreamingSql`, so the fake server of the replay tests turns the `ResultSet` of such an
exchange into the one message of a stream. It sends a recorded stream as it is. The other tests of
the package use fake servers that the tests start, and a key that `crypto/rsa` makes for each run.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was written on
2026-10-10 from the staged code. A fact of Couchbase comes from [COUCHBASE.md](COUCHBASE.md), and
a fact of Spanner from the sections above.

### The server

| | Couchbase | Spanner |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /v1/{session}:executeStreamingSql`, with `sql`, `params`, `paramTypes`, `transaction` and `seqno`. A session comes first, and a DDL statement goes to `PATCH /v1/{database}/ddl` (Requests) |
| Database | The key `query_context` of the body | The path of the request: `projects/{project}/instances/{instance}/databases/{database}` (Requests) |
| Language | SQL++, which is close to SQL | GoogleSQL, one statement for each request. `MERGE` and `TRUNCATE TABLE` are refused (Statements) |
| DDL | In SQL++ | In its own call, `updateDatabaseDdl`, which answers a long running operation that the client reads until it is done. `executeSql` refuses DDL (Statements) |
| Parameters | `?`, `$1` and `$name` | `@name` only, with a type for each parameter. `?` is refused (Parameters) |
| Framing | One body for the whole result, which does not page | A JSON array of messages. Each message holds a flat list of values, and the first one holds the columns. The server splits a value of more than about 1 MiB across messages (Responses) |
| Columns | `signature`, before the first row | `metadata.rowType.fields` of the first message, which can come after the values of that message (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement (Responses) |
| Errors | Can come with HTTP 200, after some rows | HTTP 400, 403, 404, 409 or 501 with a JSON object, or HTTP 200 with an element that holds `error` after some rows. `ABORTED` is HTTP 409 with a `retryDelay` (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a type for each column. An `INT64` and a `NUMERIC` are strings, `BYTES` is base64, `FLOAT64` has three values that are strings (Types) |
| Cancel | The server stops a query when the client leaves | Not measured for a query. `operations:cancel` stops a DDL operation (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `beginTransaction` and `commit` or `rollback`, with an id of its own, `seqno` and, on a multiplexed session, a precommit token (Transactions) |
| Authentication | Basic | A Bearer token from a JWT that the client signs with the RSA key of a service account (Requests) |
| Default port | 8093, or 18093 with TLS | 443 with TLS at `spanner.googleapis.com`. The emulator serves REST on 9020 (The DSN) |

The differences that a caller sees:

- A transaction is `beginTransaction`, and an `ABORTED` answer goes to the caller as an error that wraps
  `ErrAborted`, with the delay that the server asks for. The caller runs the whole transaction again
  (D191 item 6).
- A DML statement outside a transaction needs one, so the driver begins it and commits it (D191 item 6).
- A DDL statement waits for its operation, costs a request for each poll, and cancels the operation when
  the context ends (D191 items 4 and 8).
- The driver reads every result with `executeStreamingSql`, and joins the pieces of a value that the server
  splits, so a `BYTES` value arrives whole (D191 item 11).
- A `?` becomes `@p1`, and a caller can write `@name` with `sql.Named`. A statement that mixes both forms is
  an error (D191 item 7).
- The credential is the path to a key file, never the text of a key (D191 item 5).

### The driver

| | `couchbase` | `spanner` |
| --- | --- | --- |
| Size, without tests, on 2026-10-10 | About 1300 lines in 8 files | About 3100 lines in 9 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `Project`, `Instance`, `Database`, `CredentialFile` and `Token`. The DSN has the keys `credential_file` and `tls` (D191) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter` and `WithDatabase`, through `WithOptions` or an argument (D109). `WithTimeout` with a positive value fails with `dbimp.ErrNotSupported`. `WithReadonly(true)` runs the statement in a read-only transaction. `WithParameter` sets a member of the body of the statement, and of `beginTransaction` and `commit` for a transaction |
| Arguments | Sent to the server as `args` and `$name` | Named parameters with a type for each: `INT64`, `FLOAT32`, `FLOAT64`, `NUMERIC`, `BOOL`, `STRING`, `BYTES`, `DATE`, `TIMESTAMP`, `JSON`, `UUID`, `INTERVAL` and `ARRAY`. A slice is an `ARRAY`, a map is `JSON`. A NULL has no type (`spanner/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own, which reads the messages of the array, joins the pieces of a value, and cuts the flat list of values by the number of columns (`spanner/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypePrecisionScale` for a `NUMERIC` column. The metadata has no length |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `int64`, `float64`, `*apd.Decimal`, `bool`, `string`, `[]byte`, `dbimp.Date`, `time.Time`, the decoded JSON value, `uuid.UUID`, `dbimp.Interval` and `[]any` (D135 and D191) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `stats.rowCountExact`, or `dbimp.ErrNotSupported` when the answer has none. `LastInsertId` always gives `dbimp.ErrNotSupported`, and `THEN RETURN` gives the key |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` calls `beginTransaction`, for read-write, read-only and repeatable read. A read-only transaction sends no commit |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds a transaction only, and the session belongs to the connector |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same for a query. A DDL statement also calls `operations:cancel` on its operation, with a limit of 5 seconds (D191 item 4) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Status, Message, RetryDelay}`, which unwraps to `*dbimp.StatusError`, and the sentinels `ErrAborted`, `ErrSessionNotFound` and `ErrCanceled` |
| Authentication | Basic | A JWT of the key file, signed in the package with RS256, and an access token that the driver renews five minutes before its end. The driver follows no redirect, so the token goes to the host of the DSN only (D191 items 5 and 11). A caller can pass its own token with `Config.Token` |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, the three sentinels, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a date, a decimal, a UUID and an interval too, where Couchbase gives JSON shapes
  (D135 and D191).
- A transaction keeps its context from `BeginTx` until `Commit` or `Rollback`, because `database/sql`
  gives those two no context (D45 and rule 4 of `AGENTS.md`).
- The rows of a DML statement that has no transaction of the caller hold a function that commits with
  the context of the statement, when the caller closes them or reads them to the end (D191 item 6).
- The driver sends one request for the database and one for the session at the first connection of a
  connector, and no request while a session lives (D191 items 3 and 10).
- The driver sends a request for each poll of a DDL operation, where Couchbase sends one request (D191
  item 8).
- `WithTimeout` fails with `dbimp.ErrNotSupported`, where Couchbase sends a timeout to the server (D109
  and "Cancellation and timeouts").
- `WithParameter` sets a member of the body, and a member of the request that begins a transaction or
  commits one, where Couchbase replaces a key of its one body.
- A DSN needs a path with three names and, for a server with TLS, a key file (D191 items
  5 and 12). The key `tls` defaults to false for localhost and a loopback address.
- A nil argument has no type in the request, and a nil slice is a typed NULL array
  (D195 item 2 and D191 item 12).
- A lost session returns `driver.ErrBadConn`, so `database/sql` tries the statement again on a
  new session (D191 item 12).
- `RepeatableRead` sends `isolationLevel`, and returns `dbimp.ErrNotSupported` if the service
  refuses it (D191 item 12). `TestIntegrationIsolationLevels` measures it.
- The Spanner transaction and the rows of a DML statement store a context, and rule 4 of
  `AGENTS.md` names them (D194).
