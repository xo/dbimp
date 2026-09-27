# ArangoDB

This file holds what is known about the HTTP interface of ArangoDB, for a
driver that D73 places after InfluxDB and CrateDB. Its work item comes when
its turn comes. The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-28,
are these:

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
- "The dbmeta entry" is `container/arangodb.go` in `dbmeta`. It is staged,
  and not committed.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.1-pro-preview`, because `gemini-3.8-flash` timed out
  or closed the connection each time that it was asked. "DeepSeek" is
  `deepseek-flash`.

## Summary

- The product is ArangoDB, a database for documents and graphs, with the
  query language AQL (the AQL manual).
- The latest release is 3.12.12 (GitHub and Docker Hub). The Community
  Edition includes every feature of the Enterprise Edition from 3.12.5 (the
  release notes).
- The licence (the release notes and `LICENSE`):
  - The source is under the Business Source License 1.1.
  - The Community Edition binaries and the official images are under the
    ArangoDB Community License. It limits a dataset to 100 GiB and forbids
    commercial use in production.
  - The dbmeta entry says that the licence is free for development and
    testing, which dbmeta D90 counts, and that the image asks for no
    acceptance at start.
- `dburl` has no scheme for ArangoDB. `usql` has no driver for ArangoDB, and
  `usql/go.mod` names no ArangoDB package. So D24 does not apply.
- `dbmeta` has no model for ArangoDB. The dbmeta entry adds the release
  `3.12.12` of the image `docker.io/library/arangodb` in the Tested tier, on
  port 8529. Its comment says that dbmeta D112 settles it. That decision is
  staged in `dbmeta`, and not committed.
- R, H and S:
  - R holds, because the Tested release passed `dbrun test` (below).
  - H is likely, because every call is HTTP, with JSON (the HTTP manual). It
    is not measured.
  - S holds, because Ken decided on 2026-09-28 that AQL meets S (D75). So
    ArangoDB leaves the list of targets in [TARGETS.md](TARGETS.md) that fail
    S. AQL has `FOR`, `FILTER`, `SORT`, `LIMIT`, `COLLECT`, `RETURN`,
    `INSERT`, `UPDATE`, `REPLACE`, `REMOVE` and `UPSERT` (the AQL manual).
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `arangodb-3.12.12`. The database is
  `dbmeta`. `dbmeta_user` reads and writes it at `/_db/dbmeta/_api/...`, gets
  HTTP 401 on `_system`, and gets HTTP 403 when it makes a database. The DSN
  of the entry has no path until step 9 settles the URL.

## Requests

- A query is `POST /_db/<database>/_api/cursor`, with a JSON body that holds
  `query` and, as options, `bindVars`, `count`, `batchSize`, `ttl`,
  `memoryLimit` and `options` (the HTTP manual). Without `/_db/<database>`,
  the path reaches the database `_system` (the HTTP manual).
- `options` holds, among others, `stream`, `fullCount`, `maxRuntime`,
  `failOnWarning`, `maxWarningCount`, `allowRetry`, `profile` and
  `skipInaccessibleCollections` (the HTTP manual).
- The server takes JSON or VelocyPack, a binary format of the vendor
  (the HTTP manual). TARGETS.md names the media types `application/json` and
  `application/x-velocypack`. 3.12 removed VelocyStream, the binary protocol
  of the vendor, and VelocyPack stays as a format over HTTP (the release
  notes).
- The server takes HTTP/1.1 and HTTP/2, and HTTP/2 with prior knowledge on a
  connection with no TLS (the HTTP manual).
- A request with a body must carry `Content-Length`. The server refuses a
  body with `Transfer-Encoding: chunked` (the HTTP manual).
- The largest body is 1 GB, the longest URL is 16K, and the headers of one
  request are at most 1 MB (the HTTP manual).
- Authentication is HTTP Basic, or a bearer JWT (the HTTP manual):
  - `POST /_open/auth` with `username` and `password` returns `jwt`. The
    token expires after one hour by default, from
    `--server.session-timeout` (the HTTP manual and the options).
  - An access token of a user can stand in for the password, in Basic or in
    `/_open/auth` (the HTTP manual).
  - Authentication is on by default, from `--server.authentication` (the
    options).
- The default port is 8529 (the HTTP manual and the dbmeta entry).
- `GET /_db/<database>/_api/version` returns `server`, `license`, `version`,
  `apiVersions`, `deprecatedApiVersions` and `requestedApiVersion`. The
  `license` field is `enterprise` for both editions in the official images
  (the HTTP manual). The AQL function `VERSION()` also returns the version
  (the AQL manual).

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has no scheme yet, and D5
  gives the scheme and the aliases to `dburl`.
- The HTTP path names the database, as `/_db/<database>/`, and the database
  is `_system` without it (the HTTP manual).
- The dbmeta entry writes the DSN `http://<user>:<password>@127.0.0.1:<port>`,
  with no path, and it makes the database `dbmeta`. So the DSN does not name
  the database that the ordinary user can reach.

## Responses

- A new cursor answers HTTP 201, and a next batch answers HTTP 200 (the HTTP
  manual). The body is one JSON object with `result`, `hasMore`, `error`,
  `code` and `cached`, and, when they apply, `id`, `count`, `nextBatchId`,
  `extra` and `planCacheKey` (the HTTP manual).
- `result` is an array of the values of one batch. Each value is what
  `RETURN` gave: a document, an object, a scalar or an array (the AQL
  manual). The response holds no list of columns (the HTTP manual).
- The examples of the HTTP manual show `result` before `hasMore` in some
  responses and after it in others. So the order of the fields is not
  given.
- Paging (the HTTP manual):
  - `batchSize` is 1000 by default, and 0 is refused.
  - If `hasMore` is true, `id` names a cursor. The next batch is
    `POST /_db/<database>/_api/cursor/<id>`. `PUT` on the same path is
    deprecated.
  - `nextBatchId` names the next batch, in every batch but the last, from
    3.11.1. `POST /_api/cursor/<id>/<batch-id>` fetches that batch. With
    `allowRetry`, the same path fetches the latest batch again. The schema
    gives `nextBatchId` as a string, and an example shows the number 2.
  - A batch can be empty while `hasMore` is true.
  - After the last batch the server deletes the cursor, unless `allowRetry`
    is true. A fetch after that is HTTP 404 with `errorNum` 1600, "cursor not
    found".
  - `DELETE /_db/<database>/_api/cursor/<id>` deletes a cursor before its
    end. It answers HTTP 202, or HTTP 404 for an unknown cursor.
  - `ttl` is the life of a cursor in seconds, renewed on each access. The
    default is 30 seconds on a single server and 600 on a Coordinator of a
    cluster.
- `stream` (the HTTP manual):
  - With `stream` false, the default, the server computes the whole result
    and holds it in memory before the first batch.
  - With `stream` true, the server computes each batch when the client asks
    for it. `extra`, with its warnings and statistics, comes only in the last
    batch.
  - A streaming query can fail after some batches arrived.
  - `count`, `fullCount` and `cache` do not work with `stream`.
- `count` true adds `count`, the number of values in the whole result (the
  HTTP manual).
- `extra.stats` holds `writesExecuted`, `writesIgnored`, `scannedFull`,
  `scannedIndex`, `filtered`, `executionTime`, `peakMemoryUsage` and more.
  `fullCount` is in it when that option is set (the HTTP manual).
- A query that writes and has no `RETURN` gives an empty `result` (the AQL
  manual).
- The server compresses a response only if `--http.compress-response-threshold`
  is above 0 and the request sends `Accept-Encoding: gzip` or `deflate`
  (the release notes). The default of that option is 0 (the options). So by
  default the server does not compress.
- In a cluster, a Coordinator forwards a request for a cursor to the
  Coordinator that holds it, and names that one in
  `x-arango-request-forwarded-to` (the HTTP manual). No source names a
  redirect.

These are the shapes of an AQL result, and the rule of D18 that each one is
nearest to. Step 9 chooses:

- Rule 1 does not apply, because the server sends no column metadata.
- An object that the query makes, such as `RETURN {a: u.a, b: u.b}`, falls
  under rule 2. Whether the keys keep the order of the query is not known
  (see Second opinions).
- A whole document, such as `RETURN u`, and a path of a traversal, fall under
  rule 3.
- Documents of one collection can have different attributes (the AQL
  manual). So `RETURN u` under rule 2 can meet a key that only a later row
  has.
- A scalar, such as `RETURN u.name`, and an array, such as `RETURN [1, 2]`,
  are not named by any rule. Rule 3 is the nearest.
- One query can return values of different shapes in one result (the AQL
  manual).
- A write with no `RETURN` gives no rows and no columns. `tblfmt` writes
  such a result set as `psql` does from `v0.19.1` (tblfmt D31, and Q12 in
  [PLAN.md](PLAN.md)).

## Types

Step 10 writes the type table. These are the types that the sources give:

- AQL has null, boolean, number, string, array and object (the AQL manual).
- A number is a signed 64-bit integer or an IEEE 754 double inside the
  server (the AQL manual). VelocyPack has more numeric types than JSON (the
  release notes).
- From 3.11.12 and 3.12.3, a double from 2^53 up to 2^64 arrives in JSON as
  an integer with `.0` after it, such as `1152921504606846976.0`. Other
  integral values arrive with no `.0` (the release notes). D19 applies.
- A string is UTF-8. AQL has no binary type, and an application stores
  binary data as base64 (the AQL manual).
- AQL has no date type. A date is a number of milliseconds since the epoch
  or an ISO 8601 string. The range is from `-62167219200000` to
  `253402300799999` (the AQL manual).
- No source names a decimal type or a UUID type.
- An attribute that does not exist reads as null (the AQL manual). So in a
  projection a missing attribute and null arrive as one value. In a whole
  document, a missing attribute is not in the object.
- An array or an object can nest to a depth a little below 200 (the AQL
  manual).
- Each document has `_key`, `_id` and `_rev`, as strings (the documents
  concept in `site/content/arangodb/3.12/concepts/`).

## Parameters

- A query names a value parameter `@name`, and a collection parameter
  `@@name`. `bindVars` holds `name` for `@name`, and `@name` for `@@name`
  (the HTTP manual and the AQL manual).
- A name starts with a letter or a digit, and then holds letters, digits and
  underscores (the AQL manual). So `@1` is a name by the manual (see Second
  opinions).
- A parameter can stand for a value or an attribute name, or an array of
  names for a path of attributes. It cannot stand for a keyword or a function
  (the AQL manual).
- Every parameter in the query must have a value, and a value for a name that
  the query does not hold is an error (the AQL manual). The errors are 1551
  and 1552 (`errors.dat`).

## Transactions

- One AQL query runs as a transaction of its own, with exceptions (the HTTP
  manual). `intermediateCommitSize` and `intermediateCommitCount` make a
  large query commit in parts (the HTTP manual).
- A stream transaction (the HTTP manual and the transactions manual):
  - `POST /_db/<database>/_api/transaction/begin` starts it, with
    `collections`, which holds `read`, `write` and `exclusive`. It answers
    HTTP 201 with `result.id` and `result.status` `running`.
  - The header `x-arango-trx-id` puts a cursor request in the transaction.
  - `PUT /_db/<database>/_api/transaction/<id>` commits, and `DELETE` on the
    same path aborts. `GET` on that path gives the status, and
    `GET /_db/<database>/_api/transaction` lists them.
  - A collection that the transaction writes must be named in `write` or
    `exclusive` at the start. A collection that it only reads is added when
    it is used, because `allowImplicit` is true by default.
  - A failed operation does not abort the transaction.
  - The transaction ends after 60 seconds with no operation, by default. The
    limit is at most 120 seconds, from `--transaction.streaming-idle-timeout`.
  - The size of one transaction is at most 512 MiB by default.
  - One transaction takes one request at a time. Concurrent requests can
    fail with error 28, "locked".
  - A cursor request in a transaction that the server does not know is HTTP
    404. A cursor request in a transaction that ended or timed out is HTTP
    410.
- A cluster gives local snapshot isolation (the transactions manual). No
  source read here names the isolation of a single server.
- A JavaScript transaction is `POST /_db/<database>/_api/transaction`, with a
  JavaScript function in `action`. It is deprecated from 3.12.0 and removed in
  4.0 (the HTTP manual).

## Errors

- An error body is `{"error": true, "code": <status>, "errorNum": <n>,
  "errorMessage": <text>}` (the HTTP manual). The vendor says to compare
  `errorNum` and not the message (the error codes page of the HTTP manual).
- The status of the cursor API (the HTTP manual):
  - HTTP 400 for a body that is not JSON, a missing query, or a query that
    is not valid. A parse error is `errorNum` 1501 (`errors.dat`).
  - HTTP 404 for a collection that does not exist, `errorNum` 1203, or for
    an unknown transaction.
  - HTTP 405 for a method that the path does not take.
  - HTTP 410 when a server of the query stops answering but the connection
    stays open.
  - HTTP 500 when `usePlanCache` is true and the query cannot use the plan
    cache.
  - HTTP 503 when a server of the query is down.
- A failed authentication is HTTP 401 with `errorNum` 11,
  `"not authorized to execute this request"` (the error codes page of the
  HTTP manual). The header `Www-Authenticate` comes with it, unless the
  request sends `X-Omit-Www-Authenticate` (the HTTP manual).
- These are other values of `errorNum` (`errors.dat`):

  | errorNum | Name |
  | --- | --- |
  | 1200 | `ERROR_ARANGO_CONFLICT` |
  | 1202 | `ERROR_ARANGO_DOCUMENT_NOT_FOUND` |
  | 1210 | `ERROR_ARANGO_UNIQUE_CONSTRAINT_VIOLATED` |
  | 1228 | `ERROR_ARANGO_DATABASE_NOT_FOUND` |
  | 1500 | `ERROR_QUERY_KILLED` |
  | 1553 | `ERROR_QUERY_BIND_PARAMETER_TYPE` |
  | 1562 | `ERROR_QUERY_DIVISION_BY_ZERO` |
  | 1579 | `ERROR_QUERY_ACCESS_AFTER_MODIFICATION` |
  | 1601 | `ERROR_CURSOR_BUSY` |
  | 1652 | `ERROR_TRANSACTION_UNREGISTERED_COLLECTION` |
  | 1655 | `ERROR_TRANSACTION_NOT_FOUND` |
- A warning comes in `extra.warnings`, with `code` and `message`, and the
  query succeeds. `failOnWarning` makes a warning an error, and
  `maxWarningCount` is 10 by default (the HTTP manual).
- A streaming query can fail after some batches, so the error comes in the
  response to a later batch (the HTTP manual). The status of that response
  is not given.
- No source names an error in a body sent with HTTP 200.
- Limits on the rate (the HTTP manual):
  - HTTP 503 when the queue of the server is full, and during start or
    shutdown. The manual says that a client can retry, and that the request
    is not always safe to run twice.
  - HTTP 412 with `errorNum` 21004 when the request sends
    `x-arango-queue-time-seconds` and the queue time of the server is
    longer. Each response carries `x-arango-queue-time-seconds`.

## Cancellation and timeouts

- A request that is running goes on to its end when the client closes the
  connection. So a closed connection does not stop a long query (the HTTP
  manual).
- `DELETE /_db/<database>/_api/query/<query-id>` sets the kill flag of a
  running query, and the query stops at its next point of cancellation. It
  answers HTTP 200, or HTTP 404 for an unknown query. `all=true` works only
  in `_system` and for a superuser (the HTTP manual).
- `GET /_db/<database>/_api/query/current` lists the running queries, with
  `id`, `database`, `user`, `query`, `bindVars`, `started`, `runTime`,
  `state` and `stream` (the HTTP manual). Query tracking is on by default
  (the options). The cursor response holds no query id (the HTTP manual, and
  both models).
- `maxRuntime` kills a query after that number of seconds. Its default,
  `--query.max-runtime`, is 0, which means no limit (the HTTP manual and the
  options).
- A cursor that nobody reads expires after `ttl` (the HTTP manual).
- `x-arango-async: store` runs a request as a job, and
  `PUT /_db/<database>/_api/job/<id>/cancel` cancels it. The cancel works only
  on JavaScript code, and AQL runs in C++ or JavaScript by the functions that
  it uses (the HTTP manual).

## Statements

- A query string holds one query. A semicolon is not allowed (the AQL
  manual).
- The AQL manual says that the parser returns an error for more than one
  write operation in one query.
- A comment is `//` to the end of the line, or `/* */`, which does not nest
  (the AQL manual).

## Principals

- The server has the user `root`, which cannot be removed (the user
  management manual in `site/content/arangodb/3.12/operations/`). The dbmeta
  entry sets its password with `ARANGO_ROOT_PASSWORD`.
- A database has the levels Administrate, Access and No access. A collection
  has Read/Write, Read Only and No Access (the user management manual):
  - Access on the database, with Read/Write on the collection, lets a user
    read and write documents.
  - Creating a collection or an index needs Administrate on the database.
  - Creating a database or a user needs Administrate on `_system`.
- The dbmeta entry makes `dbmeta_user` with the level `rw` on the database
  `dbmeta` and nothing on `_system`. It runs `arangosh` in the container, and
  it can run twice.
- The version endpoint needs read access to the database in the path, and
  `details=true` needs Administrate on `_system` (the HTTP manual). So the
  ordinary user asks `/_db/dbmeta/_api/version`.
- Whether an ordinary user can list and kill its own queries is not known
  (see Second opinions).
- `skipInaccessibleCollections` makes a query read a collection that the user
  cannot read as empty, where it is an error otherwise (the HTTP manual).

## Flavors

- No source names another product that speaks this API.
- `arangodb/enterprise` on Docker Hub has the same tags as the official
  image, 3.12.12 among them. `arangodb/arangodb` on Docker Hub has no tag
  after 3.12.4.3 and 3.11.14, from 2025 (Docker Hub).
- The official image `docker.io/library/arangodb` names only 3.12. Its
  `3.11.14` was last built on 2025-05-24 (the dbmeta entry).

## Interfaces

Step 10 writes the interface table from the code.

## Faults

- `usql` has no ArangoDB driver, so there is no fault of `usql` to record.
- These are faults of the Go driver, which a driver here does not repeat
  (the Go driver):
  - `Close` on a cursor deletes it with `context.Background`
    (`cursor_impl.go`).
  - It decodes each batch whole, with `json.Unmarshal` (`cursor_impl.go`).
    D25 asks for one token at a time.
  - Nothing in `cursor_impl.go` kills the query when the context ends.

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

## Open questions

- None about S: Ken decided on 2026-09-28 that AQL meets S (D75).
- A stream transaction needs its write collections at the start, and
  `BeginTx` does not know them. Step 9 decides between a transaction and the
  error of D20. A form that stretches the contract is Ken's to decide
  (DRIVER.md, "Where you stop and ask Ken").
- JSON or VelocyPack, and whether VelocyPack needs Ken's approval as a binary
  encoding for this driver (D13), are decisions of step 9.
- Step 9 decides the rule of D18 for each shape of result. It also names
  the one column of rule 3, and the rule for a scalar or an array.
- The server does not stop a query when the client leaves. How the driver
  stops one, by `DELETE` on the cursor or on the query, is a decision of step
  9 under D36.
- The URL of the DSN and how it names the database are decisions of step 9.
  The scheme and the aliases belong to the `dburl` session (D5).
- None about the dbmeta entry: its release passed `dbrun test`, and
  dbmeta D112 now exists, staged and not committed. Its DSN names no
  database until step 9 settles the URL.
