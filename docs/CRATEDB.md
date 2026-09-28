# CrateDB

This file holds what is known about the HTTP interface of CrateDB. Ken
decided on 2026-09-29 that dbimp writes no driver for it (D88). `usql`
reaches CrateDB with `pgx` on the PostgreSQL wire protocol, and `dbmeta`
reads it through a dialect of its own. W12 in [BACKLOG.md](BACKLOG.md) is
closed. The file stays as the record of why. The headings are the template
of [DRIVER.md](DRIVER.md).

## The wire protocol

The `dbmeta` session measured the PostgreSQL wire protocol of 6.4.5 on
2026-09-29, through `dbrun` with port 5432 published, with `pgx` v5 and
`lib/pq`, as the user `crate`:

- The extended protocol with `$1` parameters, prepared statements, a
  pipelined batch of `pgx`, DDL and DML work.
- Bigint, double, bool, timestamp, `now()`, arrays, ip and geo_point scan. An
  object arrives as the bytes of JSON.
- `SHOW server_version` and `current_setting('server_version')` answer 14.0,
  and `server_version_num` answers 140000. `version()` answers
  `CrateDB 6.4.5 ...`.
- A transaction fails on both drivers. `pgx` sends ROLLBACK, which the
  parser refuses, and `lib/pq` fails at BEGIN with "unexpected transaction
  status idle".
- COPY of `pgx` fails, because the server refuses the binary form of COPY.
  LISTEN is a parse error.
- A timeout of the context ended a call after 300 ms, which `pgx` did from
  its side. Whether the server stopped the query is not measured.
- The postgres model of `dbmeta` runs 11 of its 55 queries. The other 44 fail
  on tables and functions of `pg_catalog` that CrateDB lacks, such as
  `pg_cast`, `pg_trigger` and `format_type`.

Not measured: whether the wire protocol streams a result, or builds it in
memory as the HTTP interface does.

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-28,
are these:

- "The HTTP manual" is `docs/interfaces/http.rst` in
  `github.com/crate/crate`, at `master` and at the tags 6.4.5 and 6.3.7.
- "The reference" is the other files under `docs/` in the same repository,
  at `master`. Each fact names its file.
- "The source" is the Java code of the same repository, at `master`. It
  names each file. `SqlHttpHandler.java` at `master` is the same as at 6.4.5.
- "The dbmeta entry" is `container/cratedb.go` in `dbmeta`. It is staged in
  `dbmeta`, and no commit holds it yet.
- "go-crate" is `github.com/herenow/go-crate`, a `database/sql` driver that
  speaks HTTP. Its last push was on 2023-06-06 (GitHub).
- "The Python client" is `crate/crate-python` 2.3.0, the official client,
  which speaks HTTP: `src/crate/client/http.py` and `converter.py`.
- "GitHub" and "Docker Hub" are the tags and releases that each one listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`.

## Summary

- CrateDB is a distributed SQL database that stores documents. It takes SQL
  over HTTP at `POST /_sql` (the HTTP manual), and it also speaks the
  PostgreSQL wire protocol (`docs/interfaces/postgres.rst`). Its licence is
  Apache 2.0 (GitHub).
- The latest release is 6.4.5, from 2026-09-16 (GitHub). The official image
  is `docker.io/library/crate`. On Docker Hub, 6.4.5 was rebuilt on
  2026-09-21 and 6.3.7 on 2026-09-03. The vendor also builds nightly images
  of 6.5.0 as `crate/crate:nightly` (Docker Hub).
- The dbmeta entry names the releases 6.3.7 and 6.4.5 in the Tested tier,
  with the product name `cratedb`. It chose them by
  `dbmeta/docs/EVALUATION.md`: 6.3 is the floor and 6.4 is the ceiling. It
  cites dbmeta D112, which is staged and not committed.
- R holds, because each Tested release passed `dbrun test` (below). H is
  likely, because every statement is one HTTP request (the HTTP manual). S
  holds, because the language is SQL (the HTTP manual).
  [TARGETS.md](TARGETS.md) places CrateDB in P1. H and S are not measured.
- `dburl` has no scheme for CrateDB, and no alias of `postgres` names it
  (`dburl/scheme.go`). The backlog of `dburl` waits for this driver to name
  its scheme (`dburl/docs/BACKLOG.md`).
- `usql` has no driver for CrateDB (`usql/drivers/`). A person can point a
  `postgres` URL at the PostgreSQL port of CrateDB, which `usql` serves with
  `pgx`. Whether that works is not measured. So a driver here adds a
  database to `usql`, and it replaces no driver (D24).
- `dbmeta` has no model for CrateDB (the dbmeta entry).
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `cratedb-6.3.7` and `cratedb-6.4.5`. The
  entry turns on host based authentication, so the password of `dbmeta_user`
  is checked, and a wrong password and an unknown user are refused. `crate`
  has no password, and a request with no credentials runs as `crate`.

## Requests

- A statement is `POST /_sql` with `Content-Type: application/json` and the
  body `{"stmt": "..."}` (the HTTP manual). The handler takes any path that
  starts with `/_sql` (`SqlHttpHandler.java`).
- The body can also hold `args`, an array of the values of the parameters,
  or `bulk_args`, an array of such arrays (the HTTP manual). A body with both
  is an error (`SqlHttpHandler.java`).
- The query parameter `types` adds `col_types` to the response. The source
  takes `?types` and `?types=true`, and nothing else
  (`SqlHttpHandler.java`). The query parameter `error_trace` adds the stack
  trace to an error (the HTTP manual).
- The header `Default-Schema` sets the schema of the session. Without it, the
  schema is `doc` (the HTTP manual). The source reads the header only when it
  makes a session (`HttpHandler.java`). So a later request on the same
  connection with another `Default-Schema` keeps the first schema.
- Authentication is HTTP Basic, a JWT as `Authorization: Bearer`, or a client
  certificate over TLS (`docs/admin/auth/methods.rst`). JWT works only over
  HTTP.
- The HTTP port is the first free port in `4200-4300`, and the PostgreSQL
  port is the first free port in `5432-5532` (`docs/config/node.rst`). The
  dbmeta entry publishes 4200.
- TLS on HTTP is off by default (`ssl.http.enabled`, `docs/config/node.rst`).
  A comment in the source says that HTTP and HTTPS use the same port
  (`MainAndStaticFileHandler.java`).
- `GET /` with `Accept: application/json`, or from a client that is not a
  browser, returns JSON with `ok`, `status`, `name`, `cluster_name` and
  `version`, which holds `number`, `build_hash`, `build_timestamp`,
  `build_snapshot` and `lucene_version`. It is HTTP 503 when the cluster has
  no master (`MainAndStaticFileHandler.java`).
- The largest body of a request is 100 MB by default
  (`http.max_content_length`, `HttpTransportSettings.java`). The longest
  statement is 262144 characters (`statement_max_length`,
  `docs/config/node.rst`).

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has no scheme for it yet.
- The DSN of the dbmeta entry is `http://crate@127.0.0.1:<port>` for the
  administrator, and `http://dbmeta_user:<password>@127.0.0.1:<port>` for
  the ordinary user. Its scheme is `http`, which D35 does not allow here.
- go-crate registers `crate`, and takes an `http` URL, of which it keeps the
  scheme, the host and the user (go-crate, `crate.go`). The image and the
  repository say `crate`, and the product and the dbmeta entry say
  `cratedb`. Ken decided on 2026-09-28 that the package and the name are
  `cratedb` (D76).
- The facts that a URL must carry are the host, the port, the user, the
  password or a token, TLS, and the schema of `Default-Schema`.

## Responses

- A statement with rows returns `cols`, then `col_types` if `?types` was
  given, then `rows`, `rowcount` and `duration` (the HTTP manual and
  `RestResultSetReceiver.java`). So the names and the types of the columns
  arrive before the first row, and rule 1 of D18 applies.
- `rows` is an array of arrays, one value for each column, in the order of
  `cols` (the HTTP manual).
- A statement with no rows returns `cols` as `[]`, `rows` as `[[]]`, and the
  row count in `rowcount` (`RestRowCountReceiver.java`). A `rowcount` of -1
  means that the server cannot count the rows (the HTTP manual).
- `duration` is a number of milliseconds, as a float
  (`ResultToXContentBuilder.java`).
- A bulk request returns `cols` as `[]`, `duration`, and `results`, with one
  `rowcount` for each set of arguments, in order (the HTTP manual).
- The server builds the whole response in memory before it sends any of it.
  It says "Incremental result streaming not supported via HTTP", and it
  counts the result against the query circuit breaker under the label
  `http-result` (`SqlHttpHandler.java` and `RestResultSetReceiver.java`).
  The response has a `Content-Length` (`SqlHttpHandler.java`). The query
  breaker is 60% of the heap by default (`indices.breaker.query.limit`,
  `docs/config/cluster.rst`). What a result larger than the breaker returns
  is not measured.
- The HTTP interface has no paging. A cursor comes from `DECLARE`, and `FETCH`
  reads its rows. A cursor `WITH HOLD` lives as long as the connection, and a
  cursor `WITHOUT HOLD` lives until `COMMIT` or `END`
  (`docs/sql/statements/declare.rst` and `fetch.rst`).
- The server keeps one session for each TCP connection, and closes it when
  the connection closes. It makes a new session when the authenticated user
  changes (`HttpHandler.java`, and `Netty4HttpServerTransport.java`, which
  makes one handler for each connection). So a session setting and a cursor
  `WITH HOLD` can outlive one request on a connection that stays open. This
  is not measured, and Gemini and DeepSeek say the opposite.
- The response is compressed only when `http.compression` is true, which is
  not the default (`HttpTransportSettings.java`).
- `GET /admin` redirects to `/` (`MainAndStaticFileHandler.java`). Whether
  `/_sql` ever redirects is not measured.

## Types

Step 10 writes the type table. `col_types` gives one number for each column.
An array is a list of 100 and the type of its elements, and the elements can
be arrays too (the HTTP manual). The numbers are these (the HTTP manual,
unless a line says otherwise):

| ID | Type | How a value arrives |
| --- | --- | --- |
| 0 | NULL | `null` |
| 1 | Not supported | not measured |
| 2 | CHAR in the HTTP manual, `byte` in the source (`ByteType.java`) | a number (`XContentBuilder.java`) |
| 3 | BOOLEAN | a JSON boolean |
| 4 | TEXT | a JSON string |
| 5 | IP | a string (the Python client) |
| 6 | DOUBLE PRECISION | a JSON number |
| 7 | REAL | a JSON number |
| 8 | SMALLINT | a JSON number |
| 9 | INTEGER | a JSON number |
| 10 | BIGINT | a JSON number |
| 11 | TIMESTAMP WITH TIME ZONE | milliseconds since the epoch, as a number (the HTTP manual and the Python client) |
| 12 | OBJECT | a JSON object (not measured) |
| 13 | GEO_POINT | `[x, y]` (`ServerXContentExtension.java`) |
| 14 | GEO_SHAPE | not measured |
| 15 | TIMESTAMP WITHOUT TIME ZONE | milliseconds since the epoch, as a number (the Python client) |
| 16 | Unchecked object | not measured |
| 17 | INTERVAL | text written by `IntervalType.PERIOD_FORMATTER` (`ServerXContentExtension.java`) |
| 18 | ROW | a list (`docs/general/ddl/data-types.rst`) |
| 19 | REGPROC | the name, as a string (`ServerXContentExtension.java`) |
| 20 | TIME | `[microseconds since midnight, offset in seconds]` (the source and the Python client) |
| 21 | OIDVECTOR | not measured |
| 22 | NUMERIC | a JSON number with every digit (`XContentBuilder.java`) |
| 23 | REGCLASS | the OID, as a number (`ServerXContentExtension.java`) |
| 24 | DATE | milliseconds since the epoch, as a number (the HTTP manual) |
| 25 | BIT | the prefixed form of `BitString.asPrefixedBitString` (`ServerXContentExtension.java`) |
| 26 | JSON | not measured |
| 27 | CHARACTER | a JSON string |
| 28 | FLOAT VECTOR | not measured |
| 29 | UUID | a string (`XContentBuilder.java` and the Python client) |
| 30 | REGTYPE | the name, as a string (`ServerXContentExtension.java`) |
| 100 | ARRAY | a JSON array |

- The HTTP manual of 6.3.7 does not list 29 and 30. `UUIDType.java` at
  6.3.7 has the ID 29. `RegtypeType.java` does not exist at 6.3.7, and 6.4.0
  added `regtype` (`docs/appendices/release-notes/6.4.0.rst`).
- The reference says that TIME arrives as milliseconds since midnight, and
  its example shows `[46800000000, 0]` for `13:00:00`, which is a number of
  microseconds (`docs/general/ddl/data-types.rst`). The source writes
  microseconds (`ServerXContentExtension.java`).
- The ranges are these (`docs/general/ddl/data-types.rst`): BYTE from -128
  to 127, SMALLINT 16 bits, INTEGER 32 bits, BIGINT 64 bits, NUMERIC up to
  131072 digits before the point and 16383 after it, REAL 6 digits of
  precision, DOUBLE PRECISION 15 digits, and a timestamp or a date from
  292275054 BC to 292278993 AD. A FLOAT_VECTOR holds from 1 to 2048
  elements. A TEXT column in the column store holds at most 32766 bytes.
- The reference says that a NUMERIC can lose precision over HTTP, because
  JSON numbers are limited to 53 bits (`docs/general/ddl/data-types.rst`).
  The source writes the digits of the `BigDecimal` whole. So a driver keeps
  them if it never decodes a number through `float64` (D19 and D33).
- A BIGINT past 2^53 also arrives as a JSON number (`XContentBuilder.java`),
  so D19 applies to it too.
- A timestamp arrives as milliseconds, so a value that is finer than a
  millisecond does not reach the client (the HTTP manual). A TIMESTAMP WITH
  TIME ZONE arrives as an instant, and the zone that the client sent is not
  in the response (not measured).
- NULL is distinct from 0, an empty string, an empty object and an empty
  array (`docs/general/ddl/data-types.rst`). CrateDB has no value that is
  missing, as distinct from NULL, in a row: a column that an insert omits is
  NULL. Whether a key that an OBJECT lacks reads as NULL is not measured.
- How a double NaN or an infinity arrives is not measured. DeepSeek said
  that each one is the string `"NaN"`, `"Infinity"` or `"-Infinity"`.
- CrateDB has no binary type in SQL. Blobs live in blob tables, behind
  their own handler (`HttpBlobHandler.java`), which is not SQL.

## Parameters

- A statement names each parameter as `$1`, `$2` and so on, or as `?` (the
  HTTP manual). `args` binds them in order. There are no named parameters.
- A parameter cannot stand in a subscript, such as `column[?]` (the HTTP
  manual).
- From 6.4.0, the server guesses the type of each parameter from its JSON
  value. The release notes say that a cast can then become stricter
  (`docs/appendices/release-notes/6.4.0.rst`). 6.3.7 guesses no types
  (`SqlHttpHandler.java` at 6.3.7).
- `bulk_args` runs one INSERT, UPDATE or DELETE for each set of arguments. A
  statement that returns rows cannot run in bulk (the HTTP manual).

## Transactions

- CrateDB has no transactions. Every statement commits at once, and
  `ROLLBACK` undoes nothing (`docs/appendices/compatibility.rst`).
- `BEGIN` and `COMMIT` are accepted. Their only effect is to open and close
  the scope of a cursor `WITHOUT HOLD` (`docs/sql/statements/begin.rst` and
  `commit.rst`). So a driver that sends them fakes a transaction, which D20
  forbids.
- Each row has a version number, which the reference offers for optimistic
  concurrency control in place of a transaction
  (`docs/appendices/compatibility.rst`).

## Errors

- An error is `{"error": {"message": "...", "code": 4045}}` (the HTTP
  manual). The code has four or five digits, and its first three digits are
  the HTTP status (`HttpErrorStatus.java`):

  | Codes | HTTP status |
  | --- | --- |
  | 4000 to 4008, 40000, 40010 to 40013 | 400 |
  | 4010 and 4011, which include a missing privilege | 401 |
  | 4030 to 4037 | 403 |
  | 4040 to 4049, 40410 and 40411 | 404 |
  | 4090 to 4099, 40910 and 40911 | 409 |
  | 4290 | 429 |
  | 5000 to 5006 | 500 |
  | 5030 to 5035 | 503 |

- The HTTP manual lists 5030 twice, as a query killed by `KILL` and as a
  cluster that is not available. The source gives 5005 to a query killed by
  `KILL` and 5030 to the cluster (`HttpErrorStatus.java`).
- A failed login is HTTP 401 with a body of plain text and the header
  `WWW-Authenticate: Basic realm="CrateDB Authenticator"`, and the server
  closes the connection (`HttpAuthUpstreamHandler.java`). So HTTP 401 is
  JSON for a missing privilege and plain text for a failed login.
- The server builds the whole response before it sends it. If the statement
  fails, it clears what it wrote and sends the error with its status
  (`SqlHttpHandler.java`). So a simple statement has no error after HTTP 200
  and no error after some rows.
- A bulk request is HTTP 200 when one set of arguments fails at run time.
  That result has a `rowcount` of -2 and an `error` with `code` and
  `message`. Only the first 10 errors on each shard carry an `error`. An
  error in the analysis of the statement fails the whole request (the HTTP
  manual).
- The Python client reads `error_message` from each element of `results`
  (the Python client, `http.py`). The HTTP manual and the source write
  `error` with `code` and `message`.
- A request that the thread pool rejects gets HTTP 429 with no body. Another
  fault of the server gets HTTP 500 with the message as plain text. The
  server closes the connection after each one (`MainAndStaticFileHandler.java`,
  for the requests that it handles).
- The Python client treats HTTP 503 as an error of the connection (the
  Python client). Whether HTTP 503 means that the statement did not reach
  the server is not measured.

## Cancellation and timeouts

- `KILL ALL` kills the jobs of the current user, and `KILL '<job id>'` kills
  one job. From 4.3, any user can kill their own jobs, and the superuser
  `crate` can kill any job (`docs/sql/statements/kill.rst`).
- A killed write is not rolled back, and a fast statement can finish before
  the `KILL` reaches it, while the client still gets an error that says it
  was killed (`docs/sql/statements/kill.rst`).
- The response carries no job id (`ResultToXContentBuilder.java`). DeepSeek
  said that a client finds it in `sys.jobs` by its statement. How a driver
  finds the job of its own request is not measured.
- When the connection closes, the server closes the session, and with it
  each portal and cursor (`Session.java`). It does not call
  `cancelCurrentJob`, which sends the kill that a cancel request uses. So a
  query can keep running after the client leaves. Gemini and DeepSeek said
  the same. This is not measured.
- The session setting `statement_timeout` kills a statement after that many
  milliseconds. Its default is 0, which means no limit
  (`docs/config/session.rst` and `Session.java`).
- `http.read_timeout` is 0 by default (`HttpTransportSettings.java`).

## Statements

- Each request holds one statement. `Session.parse` expects one statement in
  the text (`Session.java`). DeepSeek said that two statements with a
  semicolon between them are an error, and that a semicolon at the end can
  be accepted. Neither is measured.
- `bulk_args` runs one statement many times (the HTTP manual).
- Comments are not measured.

## Principals

- The superuser is `crate`. It has no password, and a password cannot be set
  for it. No other superuser can exist
  (`docs/admin/user-management.rst`).
- Host based authentication decides the method for each user, address and
  protocol. Without `auth.host_based`, CrateDB trusts every connection and
  takes the user from the request, if that user exists
  (`docs/admin/auth/hba.rst`).
- With the method `trust` over HTTP, the user is the name in the Basic
  header. A request with no header gets the user of
  `auth.trust.http_default_user`, which is `crate` by default
  (`docs/admin/auth/methods.rst` and `docs/config/node.rst`). So a request
  with no credentials can run as the superuser.
- The privileges are `DQL`, `DML`, `DDL` and `AL`, on the classes
  `CLUSTER`, `SCHEMA`, and `TABLE` or `VIEW` (`docs/admin/privileges.rst`).
- The dbmeta entry starts CrateDB with one node and host based
  authentication, which trusts `crate` and asks every other user for a
  password. Its `Init` makes the ordinary user `dbmeta_user`, and grants it
  `DQL`, `DML` and `DDL` on the schema `dbmeta`. CrateDB has schemas and no
  databases.
- The version is `SELECT version()` (`docs/general/builtins/scalar-functions.rst`),
  or `version.number` from `GET /` (`MainAndStaticFileHandler.java`).
  Whether an ordinary user can read each one is not measured.

## Flavors

- Gemini said that no other product speaks the `/_sql` interface of
  CrateDB.
- The PostgreSQL wire protocol is a second interface of the same product,
  and not a flavor.

## Interfaces

Step 10 writes the interface table from the code.

## Faults

`usql` has no driver for CrateDB, so no driver of `usql` has a fault here.
go-crate is the other `database/sql` driver in Go that speaks HTTP. These are
its faults, which a driver here must not repeat (go-crate, `crate.go`):

- It takes no context, and implements none of the context forms of the
  interfaces of `database/sql/driver`.
- Its `Open` writes the URL and the client into the one value that it
  registers, so every `sql.DB` in a process shares the last URL that was
  opened.
- It decodes the whole response into memory before it returns the first
  row.
- It returns every number as a `json.Number`, and an object or an array as
  the decoded JSON, never as a Go value of the type in `col_types`, which it
  asks for and does not read.
- It returns a timestamp as a number of milliseconds, and never as a time.
- An error with an HTTP status that is not 2xx loses its code, because the
  driver returns the body as text.
- `Rows.Next` stops at `rowcount`, and does not read the length of `rows`.
- Its DSN is an `http` URL, so its scheme is not the name of the driver
  (D35), and it drops the path and the query.
- `Begin` returns an error with a capital letter, and so does
  `LastInsertId`.

## Second opinions

Step 7 fills this section. For step 3, Gemini and DeepSeek were asked on
2026-09-28. Most of the requests to each one timed out, and the answers
below are the ones that came back. Each one is a lead.

- Gemini said that no other product speaks the `/_sql` interface.
- Gemini said that a client disconnect does not stop a running query.
  DeepSeek said the same, with medium confidence. The source agrees.
- Gemini and DeepSeek said that each HTTP request has a session of its own,
  so that `SET SESSION` and a cursor `WITH HOLD` do not persist to the next
  request. The source keeps one session for each TCP connection
  (`HttpHandler.java`), which disagrees.
- DeepSeek said that the server builds the whole response in memory. The
  source agrees.
- DeepSeek said that a client cancels with `KILL`, after it finds the job id
  in `sys.jobs`.
- DeepSeek said that a syntax error is HTTP 400, an unknown table is 404, an
  internal error is 500, and a failed login is 401. The source agrees.
- DeepSeek said that a NUMERIC arrives as a JSON number, and a timestamp as
  milliseconds since the epoch. The source agrees.
- DeepSeek said that a TIMETZ arrives as a string, such as
  `"12:34:56.789+02:00"`. The source and the Python client say that it is a
  list of microseconds and an offset, which disagrees.
- DeepSeek said that a double NaN arrives as the string `"NaN"`.
- DeepSeek said that one request cannot hold two statements.

## Open questions

- None about R: each Tested release of the dbmeta entry passed `dbrun test`
  (dbmeta D112).
- The server holds each whole result in memory, and paging over HTTP needs a
  cursor on one TCP connection. The second condition of "When it cannot be a
  driver" in [DRIVER.md](DRIVER.md) asks whether a result can be too large
  to hold. Step 6 measures what a large result returns, and whether a
  cursor `WITH HOLD` and `FETCH` work across requests on one connection.
  Then Ken decides.
- A session outlives a request on a connection that stays open. Whether the
  driver keeps one TCP connection for each `driver.Conn` is a decision of
  step 9.
- None about the name. Ken decided on 2026-09-28 that the driver is named
  `cratedb` (D76).
- How the driver cancels a query on the server, since the response carries
  no job id, is a decision of step 9 (D36).
- A request with no credentials can run as `crate`. Whether the driver sends
  a user every time is a decision of step 9.
