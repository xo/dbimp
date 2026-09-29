# rqlite

This file holds what is known about the HTTP API of rqlite, for a driver
that D73 places seventh after Neo4j. Its work item comes when its turn
comes. The headings are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server has run for this driver yet, so every
fact here is "not measured" and names its source. R is the exception, because
`dbrun` measured it (Summary). The sources, each read on 2026-09-27, are
these:

- "The API document" is `content/en/docs/API/api/_index.md` in
  `github.com/rqlite/rqlite.io`, at commit `58779997fc7a`. "The consistency
  document", "the bulk document", "the queue document", "the security
  document", "the rewrite document" and "the FAQ" are the files for Read
  Consistency, Bulk API, Queued Writes, Security, non-deterministic
  functions and the FAQ in the same tree.
- "The server source" is `github.com/rqlite/rqlite` at commit
  `fe6ade752262`, after v10.3.6: `http/service.go`, `http/query_params.go`,
  `http/request_parser.go`, `command/encoding/json.go`, `db/db.go`,
  `db/driver.go`, `store/store.go`, `auth/credential_store.go`,
  `Dockerfile`, `docker-entrypoint.sh` and `CHANGELOG.md`. A fact from it is
  what the code says, and not what a server did.
- "gorqlite" is `github.com/rqlite/gorqlite` at commit `50d445fd0ab9` of
  2026-05-04, with its package `stdlib`. This is the pseudo-version
  `v0.0.0-20260504155303` that the `usql` backlog names.
- "rqlite-go-http" is `github.com/rqlite/rqlite-go-http` at commit
  `b9e674c31e86` of 2026-09-08. It has no tag.
- "The dbmeta entry" is `container/rqlite.go` in `dbmeta`. It is staged,
  and not committed.
- "GitHub" and "Docker Hub" are the releases and the tags that each one
  listed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, both
  asked on 2026-09-27.

## Summary

- rqlite is a distributed database. Each node runs SQLite, and the nodes
  agree on each write through Raft (the API document). A client sends SQL
  over HTTP to any node.
- The latest release is 10.3.6, of 2026-09-22 (GitHub and Docker Hub). The
  last release of 9 is 9.4.5, of 2026-03-10, and the last release of 8 is
  8.43.4, of 2025-08-28 (Docker Hub). The changelog of 10.0.0 says that it
  has no breaking change to the API (the server source). rqlite is under the
  MIT licence (GitHub).
- R holds. The dbmeta entry names the releases 9.4.5 and 10.3.6 in the
  Tested tier, on the image `docker.io/rqlite/rqlite`, and each one passed
  `dbrun test` (below). It cites dbmeta D112, which is staged and not
  committed.
- H is likely. Each statement is a `POST` of JSON to `/db/query`,
  `/db/execute` or `/db/request` (the API document).
- S is likely, because the statement is the SQL of SQLite (the API
  document). TARGETS.md places rqlite in P1.
- `dburl` has the provisional scheme `rqlite`, with the alias `rq` and the
  default port 4001, the HTTP port. It passes the path and the query through
  (dburl D36, released in dburl `v0.36.0` as dburl D38, read on
  2026-09-29). Nothing of it
  is settled until step 9 decides the name and the URL.
- `usql` has no driver for rqlite. Its backlog lists
  `github.com/rqlite/gorqlite/stdlib` at `v0.0.0-20260504155303` as a driver
  that it can add. So a driver here is new to `usql` (D24).
- `dbmeta` has no model for rqlite. Its model for `sqlite3` reads
  `sqlite_schema` and the table valued pragmas, such as
  `pragma_table_xinfo`, and its version statement is
  `SELECT sqlite_version()` (`dbmeta/models/sqlite3`). TARGETS.md says that
  this model can read rqlite as a flavor.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `rqlite-9.4.5` and `rqlite-10.3.6`.
  `dbmeta_user` has the permissions `query` and `execute` only.

## Requests

- A write is `POST /db/execute`. A read is `POST /db/query`, or
  `GET /db/query?q=<statement>`. `/db/request` is the unified endpoint, and
  takes reads and writes in one request (the API document). It decides
  which each statement is with `sqlite3_stmt_readonly()` (the API
  document).
- `/db/query` runs every statement on a read-only connection to SQLite, so
  a write there fails (the security document).
- The body is a JSON array of statements. Each statement is a string, or an
  array whose first element is the SQL and whose other elements are the
  arguments (the API document). A body with `Content-Type: text/plain` is
  one statement (the API document). The server parses any other content
  type as JSON (the server source).
- The query options are flags and keys in the URL (the API document, the
  consistency document and the server source):
  - `level` is `none`, `weak`, `linearizable`, `strong` or `auto`. `weak`
    is the default, and an unknown level is `weak`.
  - `linearizable_timeout` is a duration, 10 seconds by default (the server
    source). `freshness` is a duration, and `freshness_strict` needs it.
  - `associative` asks for each row as an object. `blob_array` asks for a
    BLOB as an array of bytes. `qualify_columns` names each column with its
    table.
  - `transaction` runs the statements of the request in one transaction.
    `queue` queues a write, and `wait` with `timeout` waits for the queue.
  - `timings`, `pretty` and `raft_index` add to the response.
  - `db_timeout` bounds the time in the database. `timeout` bounds a request
    that one node forwards to the leader, and is 30 seconds by default.
    `retries` sets how many times a node tries to reach another node.
  - `redirect` asks for a redirect to the leader in place of forwarding.
  - `norwrandom` and `norwtime` stop the rewrite of `RANDOM()` and of the
    functions of time, and `noparse` stops every rewrite.
- A flag counts when its key is present, whatever its value. So
  `transaction=false` still asks for a transaction (the server source,
  `QueryParams.HasKey`).
- Authentication is HTTP Basic. A node reads its users and their
  permissions from a JSON file that the flag `-auth` names (the security
  document). HTTPS and mutual TLS are flags of the server (the security
  document).
- The default port of the HTTP API is 4001, and Raft uses 4002 (the API
  document, `Dockerfile` and `docker-entrypoint.sh`).
- The version is in the header `X-RQLITE-VERSION`, which the server adds to
  every response before it checks the credentials (the server source,
  `addBuildVersion`). `GET /status` also gives it, and needs the permission
  `status` (the server source). rqlite-go-http reads the header from
  `GET /status`.

## The DSN

- Step 9 decides the URL (D27 and D35). No endpoint names a database (the
  API document), so the path has no meaning yet.
- gorqlite takes `http[s]://[user[:password]@]host[:port]/[?key=value]`,
  with the keys `level`, `disableClusterDiscovery` and `timeout`. An empty
  host is `localhost:4001` (gorqlite `conn.go`). Its scheme is `http` or
  `https`, which D35 does not allow here.
- The dbmeta entry gives the DSN as `http://user:password@127.0.0.1:<port>`,
  for the administrator and for the ordinary user.

## Responses

- The body is one JSON object, with `results` first, then `error`, `time`,
  `sequence_number` and `raft_index` (the server source, `Response`). Each
  key after `results` is left out when it is empty.
- Each element of `results` is one statement, in the order of the request
  (the API document).
- In the default form, a read gives `columns`, then `types`, then `values`,
  an array of arrays in the order of `columns` (the API document and the
  server source). So the names of the columns and their order arrive before
  the rows, and rule 1 of D18 applies.
- `columns`, `types` and `values` are each left out when they are empty
  (the server source, `encoding.Rows`). So a read that finds no rows has no
  `values`.
- A write gives `last_insert_id` and `rows_affected`, and each is left out
  when it is zero (the server source, `encoding.Result`). So a write that
  changes nothing can be an empty object, or hold only `time`.
- In `/db/request`, a read and a write differ only in which keys the element
  holds (the API document and rqlite-go-http).
- The associative form writes each row as an object (the API document). The
  server builds the object from a Go map (the server source), and
  `encoding/json` writes the keys of a map in sorted order. The example in
  the API document shows the keys sorted. So that form loses the order of
  the columns, which hard rule 3 forbids.
- The server reads every row of every statement into memory, then encodes
  the whole response with `json.Marshal`, and writes it at once (the server
  source, `queryStmtWithConn` and `writeResponse`). Gemini and DeepSeek say
  the same. The files read here set no cap on rows, and no paging exists.
  So a large result costs the server its memory, whatever the driver does.
- The server compresses no response (the server source has no gzip).
- A write sent to a follower goes on to the leader, and the follower returns
  the answer of the leader (the API document). With `redirect`, the follower
  answers HTTP 301 with the leader in `Location` (the API document and the
  server source). The header `X-RQLITE-SERVED-BY` names the node that served
  the request (the server source).
- `GET /` answers HTTP 302 to `/console/` (the server source).
- A statement that the server marks `ForceQuery` runs as a query on
  `/db/execute` and returns rows (the server source). The changelog of 9.2.0
  links this to `RETURNING`.

## Types

Step 10 writes the type table. These facts come from the server source,
which runs SQLite through a fork of `mattn/go-sqlite3`,
`github.com/rqlite/go-sqlite3` v1.51.0:

- `types` holds the declared type of each column in lower case, and not its
  storage class (the API document and the server source). A column that has
  no declared type, such as an expression, takes the type of its value in
  the first row: `integer`, `real`, `boolean`, `blob` or `text`. If that
  value is NULL, the type stays empty.
- An integer is a JSON integer, from an `int64`, so it keeps every digit.
  A REAL is a JSON number from a `float64`.
- `json.Marshal` returns an error for NaN and for an infinity (the Go
  documentation of `encoding/json`). So a REAL that is infinite makes
  `writeResponse` answer HTTP 500 with the text of that error (the server
  source).
- A string is a JSON string. NULL is `null`.
- A BLOB is a string in base64 by default. With `blob_array`, it is an array
  of numbers from 0 to 255 (the API document). The API document says that in
  the default form a client cannot tell a BLOB from text, and suggests
  `STRICT` tables.
- A value of type `[]byte` becomes text when the declared type has the
  affinity of text, and the empty type counts as text (the server source,
  `isTextType`). So a BLOB from an expression, such as `SELECT x'00ff'`,
  arrives as a JSON string of its raw bytes, with the type `text`. Step 6
  measures it.
- In `mattn/go-sqlite3` v1.14.52, a column declared `date`, `datetime` or
  `timestamp` is read as a `time.Time`, and a column declared `boolean` as a
  `bool` (its `sqlite3.go`). The server writes a `time.Time` as text in the
  form of RFC 3339, and a `bool` as a JSON boolean (the server source). This
  session did not read the fork, so whether it does the same is not
  measured.
- SQLite has no decimal type and no UUID type. A decimal and a UUID arrive
  as the text, the number or the BLOB that the statement stored.

## Parameters

- A statement binds `?` from the elements after its SQL, in order (the API
  document).
- A statement binds names from an object after its SQL, such as
  `["... VALUES(:name)", {"name": "fiona"}]` (the API document). The server
  passes each key as `sql.Named` to the SQLite driver (the server source).
- The server parser takes plain and parameterized statements in one body,
  and an object and positional values in one statement (the server source).
  The bulk document says that one request cannot mix the plain and the
  parameterized forms. Step 6 measures which is true.
- The server reads a number as an `int64` if it can, and else as a
  `float64` (the server source, `makeParameter`). A boolean, NULL and a
  string bind as themselves.
- An array of numbers from 0 to 255 binds as a BLOB (the API document and
  the server source).
- A string in the form `x'...'` binds as a BLOB (the API document and the
  server source, `db.ParseHex`). So a driver cannot send text of that form
  as text through a parameter. DeepSeek said the opposite.

## Transactions

- `transaction` makes the statements of one request succeed or fail
  together (the API document and the bulk document). Each request is one
  entry of the Raft log, and no other request runs between its statements
  (the bulk document).
- The API document says that the behavior of `BEGIN`, `COMMIT`, `ROLLBACK`,
  `SAVEPOINT` and `RELEASE` is not defined. The FAQ says that they work, but
  are not supported, because a failure during the transaction can leave the
  cluster in a state that is hard to use.
- Every write over HTTP uses the same SQLite connection on the leader (the
  FAQ). So a `BEGIN` from one client is open on the connection that every
  other client writes through. What the other clients see is not measured.
- A queued write is not in a transaction unless the server starts with
  `-write-queue-tx` (the queue document).
- gorqlite returns a transaction whose `Commit` and `Rollback` do nothing
  (gorqlite `stdlib/sql.go`). D20 forbids that.

## Errors

- An error comes as an HTTP status of 4xx or 5xx, or as HTTP 200 with the
  key `error` (the API document).
- The key `error` is in the element of the statement that failed, or at the
  top of the body (the API document and the server source). An element can
  hold rows and the next element an error, so an error can come after the
  rows of an earlier statement.
- The API document says, in its section on transactions, that processing
  stops at the first statement that fails.
- The server source gives these statuses:
  - HTTP 400 for a body or a key that it cannot parse, with the reason as
    plain text.
  - HTTP 401 with no body when a user lacks a permission or the password is
    wrong. When `-auth` is set, every response has
    `WWW-Authenticate: Basic realm="rqlite"`.
  - HTTP 405 for a wrong method. `/db/request` takes only `POST`.
  - HTTP 503 with the text `leader not found`.
  - HTTP 500 when a rewrite of the SQL fails, or when the response cannot be
    encoded.
  - HTTP 408 when a queued write waits past its timeout.
- The files read here return no HTTP 429.

## Cancellation and timeouts

- A read that is not `strong` runs in SQLite with the context of the HTTP
  request (the server source, `Store.Query` and `DB.QueryWithContext`). So a
  client that disconnects cancels a read on the node that runs it. Step 6
  measures this. DeepSeek said the same, and Gemini timed out.
- A write checks the context before it starts, and then goes through Raft
  (the server source, `Store.Execute`). So a write that has started does not
  stop when the client leaves.
- rqlite has no request that cancels a running statement. `db_timeout`
  bounds the time in the database (the API document).
- The API document says that `db_timeout` applies to each statement. The
  server source applies it to each statement of a write, and to the whole of
  a read.
- By default, SQL does not time out (the API document).

## Statements

- One request holds many statements, one for each element of the array (the
  bulk document).
- Whether one string that holds two statements runs both is not measured.
  How the server treats a comment is not measured.
- The server rewrites `RANDOM()`, `RANDOMBLOB(N)` and the functions of time
  in a write, and in a `strong` read, before it writes the statement to Raft
  (the rewrite document).
- The server refuses `PRAGMA journal_mode`, `PRAGMA wal_checkpoint`,
  `PRAGMA wal_autocheckpoint` and `PRAGMA synchronous` (the API document).
  Other pragmas pass to SQLite. Whether the table valued pragmas that
  `dbmeta` reads pass is not measured.
- Foreign keys are on only when the server starts with `-fk`. The entrypoint
  of the image adds `-fk` when the environment variable `ENABLE_FK` is set
  (`docker-entrypoint.sh`).

## Principals

- The permissions are `all`, `execute`, `query`, `status`, `ready`,
  `backup`, `load`, `snapshot`, `join`, `join-read-only`, `remove`,
  `leader-ops` and `ui` (the security document). The server source also has
  `join-read-replica`.
- `/db/execute` needs `execute`, `/db/query` needs `query`, and `/db/request`
  needs both (the security document).
- The user `*` gives its permissions to every request, with or without
  credentials (the security document). A node without `-auth` checks nothing
  (the server source).
- No permission is narrower than an endpoint. A user with `execute` can
  create and drop any table (the security document).
- The server compares the password in the file as plain text (the server
  source, `CredentialsStore.Check`).
- The dbmeta entry writes two users: `admin`, with `all`, and `dbmeta_user`,
  with `query` and `execute` and nothing else. So the ordinary user can use
  all three endpoints, and cannot read `/status`. It still gets the version
  in the header `X-RQLITE-VERSION`.
- `SELECT sqlite_version()` gives the version of SQLite to either user
  through `/db/query`. Step 6 measures it.

## Flavors

- Gemini and DeepSeek name no other product that speaks the HTTP API of
  rqlite.
- libSQL is a target of its own, with its own protocol (TARGETS.md).

## Interfaces

Step 10 writes the interface table from the code.

## Faults

These are faults of the Go clients that a driver here does not repeat.

gorqlite, and its package `stdlib`, which registers `rqlite`:

- It fakes a transaction. `Begin` returns a value whose `Commit` and
  `Rollback` do nothing (`stdlib/sql.go`). D20 forbids that.
- It sends `transaction` on every request unless the caller turns it off
  (`conn.go` and `cluster.go`).
- It reads each response whole with `io.ReadAll` (`api.go`), and keeps every
  row in memory (`query.go`).
- It decodes the values with `encoding/json` into `any`, so each number
  becomes a `float64` (`query.go`). An integer larger than 2^53 loses digits
  (D19).
- It does not ask for `blob_array`, so a BLOB reaches the caller as a string
  in base64 (`query.go`).
- It turns a column declared `date` or `datetime` into a `time.Time` in two
  forms of text, and a number into whole seconds (`query.go`, `toTime`).
- It sends a request to each peer in turn after any failure, including a
  status other than 200 after the server ran the statement (`api.go`,
  `rqliteApiCall`). So a write can run twice.
- `Open` reads `/status` with `context.Background` (`cluster.go`), and the
  driver has no `DriverContext` (`stdlib/sql.go`).
- Its default `http.Client` is a package variable with a timeout of 10
  seconds, shared by every connection (`conn.go`). So a long query fails
  whatever its context says.
- Its DSN is an `http` or `https` URL, so its scheme is not the name of the
  driver (D35).
- It refuses a named parameter, which rqlite takes (`stdlib/sql.go`).
- A query always goes to `/db/query`, which is read-only, so a write with
  `RETURNING` through `QueryContext` cannot run (`stdlib/sql.go`, and the
  security document).
- It writes a trace to a writer that a package function sets (`gorqlite.go`,
  `TraceOn`). Hard rule 2 forbids that.
- It depends on the standard library only (`go.mod`).

rqlite-go-http, which has no `database/sql` driver:

- It reads each response whole with `io.ReadAll` (`http.go`).
- It decodes numbers with `UseNumber`, so an integer keeps its digits
  (`http.go`).
- Its default `http.Client` has a timeout of 5 seconds (`http.go`).
- It returns no error for a failed statement unless the caller sets
  `PromoteErrors` (`http.go`).
- A balancer that marks a host bad sends the request again to the next host
  after any error of the transport, which can come after the server received
  it (`http.go`, `doRequest`).
- Its `QueryOptions.Timeout` says that it applies in the database, but it
  sends `timeout`, which the server reads as the timeout for forwarding
  (`options.go`, and the server source).
- It depends on the standard library only (`go.mod`).

## Second opinions

Step 7 tests each lead on a server. Gemini and DeepSeek were asked the same
questions on 2026-09-27. Gemini timed out on most of them and answered four.

- A `BEGIN` in one request and a `COMMIT` in a later one. DeepSeek said that
  rqlite keeps no transaction across requests. Gemini said that it runs in
  SQLite but is not supported. It also said that other readers do not see
  the changes before the commit, and that other writers wait or get an error
  of a lock. The FAQ says that it works, and that every write shares one
  connection, which disagrees with Gemini about other writers.
- A client that disconnects. DeepSeek said that the server likely cancels
  the query. The server source agrees for a read.
- A large result. Both said that the server builds it in memory. The server
  source agrees. DeepSeek said that a cap on the size of a response can
  exist, and the files read here have none.
- NaN and infinity. DeepSeek said that they arrive as strings. The server
  encodes with `encoding/json`, which refuses them.
- `SELECT x'00ff'`. DeepSeek said that it arrives in base64 with the type
  `blob`. The server source says text of the raw bytes, with the type
  `text`.
- The string `"x'41'"` as a parameter. DeepSeek said that it binds as text.
  The API document and the server source say a BLOB.
- Other products. Both named none.
- Go clients. DeepSeek named `github.com/rqlite/rqlite-go` and
  `github.com/rqlite/rqlite-go-sql`. The first exists, with its last push on
  2022-01-06, and the second does not exist (GitHub). Gemini named gorqlite,
  whose package `stdlib` registers a `database/sql` driver, rqlite-go-http,
  and `github.com/goki/rqlite`. That last one exists, and it is a driver for
  GORM, with its last push on 2023-12-21 (GitHub).

## Open questions

- None about R: each Tested release of the dbmeta entry passed `dbrun test`
  (dbmeta D112).
- The URL of the DSN, how it asks for TLS, and its keys, such as `level`,
  are decisions of step 9.
- Whether the driver sends every statement to `/db/request`, which needs
  both permissions, or sends reads to `/db/query` and writes to
  `/db/execute`, is a decision of step 9.
- Transactions are a question for Ken. The `transaction` flag covers one
  request, and rqlite does not support `BEGIN` across requests. D20 forbids
  a faked transaction.
- A string of the form `x'...'` binds as a BLOB. How the driver sends such
  text is a decision of step 9.
- How the driver tells a BLOB from text, with `blob_array` and for an
  expression that has no declared type, is a question of steps 6 and 9.
- Whether the driver follows the redirect to the leader, or lets the
  follower forward, and to which hosts it sends the credentials, is a
  decision of step 9.
- rqlite has no namespace. How an integration test keeps its tables apart
  is a question of step 14.
