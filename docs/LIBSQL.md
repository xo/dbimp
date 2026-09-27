# libSQL

This file holds what is known about libSQL and Turso, for a driver that D73
places after rqlite. Its work item comes when its turn comes. The headings
are the template of [DRIVER.md](DRIVER.md).

This is the draft of step 3. No server ran for this draft, so every fact here
is "not measured" and names its source. R is the exception, because `dbrun`
measured it (Summary). The sources, each read on 2026-09-27, are these:

- "The Hrana spec" is `docs/HRANA_3_SPEC.md` in
  `github.com/tursodatabase/libsql`, on the branch `main`.
- "The HTTP v2 spec" and "the HTTP v1 spec" are `docs/HTTP_V2_SPEC.md` and
  `docs/HTTP_V1_SPEC.md` in the same repository.
- "The HTTP API document" is `docs/http_api.md`, "the Docker document" is
  `docs/DOCKER.md`, and "the admin document" is `docs/ADMIN_API.md`, in the
  same repository.
- "The server source" is `libsql-server/src/` and `libsql-hrana/src/` in the
  same repository. Each fact names its file.
- "The Go client" is `github.com/tursodatabase/libsql-client-go` at commit
  `9d5d30a29a60` of 2026-05-28, read with `gh api`. It is the version that
  `usql/docs/BACKLOG.md` names.
- "The Turso documents" are `docs.turso.tech/sdk/http/reference` and
  `docs.turso.tech/cli/db/tokens/create`.
- "The Turso source" is `cli/app.rs` and `cli/sync_server.rs` in
  `github.com/tursodatabase/turso`.
- "GitHub" is the releases and the tags of `tursodatabase/libsql`, and
  "ghcr.io" is the tag list of the image `ghcr.io/tursodatabase/libsql-server`.
- "The dbmeta entry" is `container/libsql.go` in `dbmeta`. It is staged,
  and not committed.
- "DeepSeek" is `deepseek-flash`.

## Summary

- libSQL is a fork of SQLite by Turso, under the MIT licence (the libSQL
  README). `libsql-server`, whose program is `sqld`, serves a libSQL
  database over HTTP and WebSocket (the libSQL README and the server source).
- Turso Cloud is a service that runs `sqld` for its customers, with the same
  HTTP API (the Turso documents). It is only a cloud service, so it fails R
  by step 2 of DRIVER.md.
- Turso Database is a new database by the same team, which rewrites SQLite
  in Rust. It was called Limbo before. The libSQL README says that libSQL is
  maintained, and that new features go into Turso Database. Turso Database
  is in beta. Its newest release is `v0.7.2` of 2026-07-30, and its newest
  prerelease is `v0.8.0-pre.13` of 2026-09-25 (GitHub). See Flavors.
- The newest release of `libsql-server` on GitHub is `libsql-server-v0.24.32`
  of 2025-02-14. The tag `libsql-server-v0.24.33`, of 2025-12-19, has no
  GitHub release, and `libsql-server/Cargo.toml` on `main` names 0.24.33
  (GitHub). The image has the tag `v0.24.33` (ghcr.io).
- The dbmeta entry names the product `libsql` and one release, 0.24.33, in
  the Tested tier. So its name in `dbrun` is `libsql-0.24.33`, by the form
  `<product>-<release>` in `AGENTS.md`. The entry cites dbmeta D112, which
  is staged and not committed.
- R holds, because the Tested release passed `dbrun test` (below). H is not
  measured, and each source says that `sqld` takes statements over HTTP. S
  is the SQL of SQLite. [TARGETS.md](TARGETS.md) places libSQL in P1.
- `dburl` has no scheme for `libsql`, `turso` or `sqld`
  (`dburl/scheme.go`). `dburl/docs/BACKLOG.md` records the question of one
  driver or two, which D76 answers: one driver, `libsql`, with `turso` as an
  alias in `dburl`.
- `usql` has no driver for libSQL (`usql/drivers/`). `usql/docs/BACKLOG.md`
  names the Go client, `github.com/tursodatabase/libsql-client-go/libsql` at
  `v0.0.0-20260528064733`, as a driver that `usql` can add. So a driver here
  adds a database to `usql`, and replaces no driver (D24).
- `dbmeta` has no model for libSQL. It has a model for `sqlite3`
  (`dbmeta/models/`). [TARGETS.md](TARGETS.md) says that the `sqlite3` model
  can read libSQL as a flavor.
- `dbrun` has an entry, staged in `dbmeta` for Ken's review and not committed,
  from dbmeta D112. The `dbmeta` session reported on 2026-09-28 that each
  Tested release passed `dbrun test`: `libsql-0.24.33`. The entry has no
  ordinary user, because it runs with basic authentication only.

## Requests

- `sqld` serves these routes on its HTTP port (the server source,
  `http/user/mod.rs`):
  - `POST /v2/pipeline`: Hrana over HTTP, version 2, in JSON.
  - `POST /v3/pipeline` and `POST /v3/cursor`: Hrana over HTTP, version 3,
    in JSON.
  - `POST /v3-protobuf/pipeline` and `POST /v3-protobuf/cursor`: the same,
    in protobuf.
  - `GET /v2`, `GET /v3` and `GET /v3-protobuf`, which return 200 with the
    text `Hello, this is HTTP API v2 (Hrana over HTTP)` for each version
    (the server source, `hrana/http/mod.rs`).
  - `POST /v1/execute` and `POST /v1/batch`: the HTTP API v1, which the HTTP
    v2 spec calls deprecated.
  - `POST /`: the first HTTP API, which the HTTP API document describes.
    `GET /` upgrades the connection to Hrana over WebSocket.
  - `GET /version`, `GET /health`, `GET /dump`, `GET /beta/listen`,
    `GET /console`, `GET /v1/jobs` and `GET /v1/jobs/:job_id`.
  - `POST /dev/:namespace/v:version/pipeline`, which the source calls a
    route for Turso development.
- [TARGETS.md](TARGETS.md) names `POST /v1/queries`. The server source has no
  such route.
- A pipeline request is `{"baton": ..., "requests": [...]}`. Each request has
  a `type` (the Hrana spec):
  - `execute`, with `stmt`, runs one statement.
  - `batch`, with `batch`, runs a list of steps, each with an optional
    `condition`. A condition is `ok`, `error`, `not`, `and`, `or` or
    `is_autocommit`.
  - `sequence`, with `sql` or `sql_id`, runs statements that semicolons
    separate, and returns no rows.
  - `describe`, with `sql` or `sql_id`, returns the parameters and the
    columns of a statement without running it.
  - `store_sql` and `close_sql` keep a text of SQL on the server under a
    number that the client chooses, for the life of the stream.
  - `get_autocommit` says whether the stream is outside a transaction. It is
    new in version 3.
  - `close` closes the stream.
- A statement is `{"sql": ..., "sql_id": ..., "args": [...], "named_args":
  [...], "want_rows": ...}`. `want_rows` is true by default. If it is false,
  the server returns no rows (the Hrana spec).
- A stream is one SQLite connection on the server. The response to each
  pipeline request holds a new `baton`, and the next request on the stream
  sends it. A `baton` of null in a request opens a new stream. A `baton` of
  null in a response means that the server closed the stream. The client
  must wait for each response before it sends the next request on a stream
  (the Hrana spec).
- The baton holds the number of the stream and a sequence number, signed by
  the server. A baton sent twice is refused (the server source,
  `hrana/http/stream.rs`).
- A response can hold `base_url`. The Hrana spec says that the client then
  sends the next requests of the stream to that URL. `sqld` sets it from
  `SQLD_HTTP_SELF_URL` (the server source, `main.rs` and `hrana/http/mod.rs`).
- `POST /v1/execute` takes `{"stmt": ...}`, and `POST /v1/batch` takes
  `{"batch": ...}`. Each runs on a new stream (the HTTP v1 spec).
- `POST /` takes `{"statements": [...]}`, where each item is a string or
  `{"q": ..., "params": ...}`. It runs every statement in one transaction
  (the HTTP API document).
- A database on one server is a namespace. The server reads the namespace
  from the header `x-namespace`, or from the first label of the `Host`
  header, or else uses the default namespace (the server source,
  `http/user/db_factory.rs`). The dbmeta entry says that the default
  namespace is named `default`. The admin API creates a namespace only when
  `sqld` runs with `--enable-namespaces` and `--admin-listen-addr` (the admin
  document).
- Authentication is one of three, by the configuration of the server (the
  server source, `main.rs`):
  - HTTP basic, from `SQLD_HTTP_AUTH`, in the form `basic:` and the base64
    of `user:password`. It is one user.
  - A JWT in `Authorization: Bearer`, which the server tests with an
    Ed25519 public key from `SQLD_AUTH_JWT_KEY` or
    `SQLD_AUTH_JWT_KEY_FILE`.
  - None.

  If `SQLD_HTTP_AUTH` is set, the server uses basic and ignores the key.
- Turso Cloud takes a token in `Authorization: Bearer` (the Turso
  documents).
- The Go client sends the header `x-libsql-client-version`, and
  `x-turso-encryption-key` when it has a key for encryption (the Go client,
  `hranaV2.go`).

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has no scheme yet.
- The Go client registers `libsql`. It takes `libsql://`, `https://`,
  `http://`, `wss://`, `ws://` and `file:` URLs. A `libsql://` URL becomes
  `https://`, or `http://` when TLS is turned off, and then an explicit port
  is required (the Go client, `sql.go`).
- `Driver.Open` of the Go client reads a token from one of the query keys
  `auth_token`, `authToken` or `jwt`, and TLS from `tls`, 0 or 1.
  `NewConnector` refuses these keys, and takes `WithAuthToken` and `WithTls`
  instead. Either one refuses any other query key (the Go client, `sql.go`).
- A database in Turso Cloud has the URL
  `https://[databaseName]-[organizationSlug].turso.io` (the Turso
  documents).
- The dbmeta entry writes `http://admin:<password>@127.0.0.1:<port>`, with
  the one user of basic authentication.
- The DSN has to name the token or the user, the namespace, and TLS. Step 9
  decides how.

## Responses

- A pipeline response is one JSON object, `{"baton": ..., "base_url": ...,
  "results": [...]}`, with `Content-Type: application/json` (the Hrana spec
  and the server source, `hrana/http/mod.rs`). Each result is
  `{"type": "ok", "response": ...}` or `{"type": "error", "error": ...}`.
  The server runs every request of the pipeline, even after an error (the
  Hrana spec).
- A statement result is `{"cols": [...], "rows": [...],
  "affected_row_count": ..., "last_insert_rowid": ..., "replication_index":
  ..., "rows_read": ..., "rows_written": ..., "query_duration_ms": ...}`
  (the Hrana spec, the server source and the Turso documents). Each column
  is `{"name": ..., "decltype": ...}`. Each row is an array of values, in the
  order of `cols`.
- The server writes the fields in the order of the struct, so `baton` comes
  before `results`, and `cols` comes before `rows` (the server source,
  `libsql-hrana/src/proto.rs`, and the snapshots in
  `libsql-server/tests/hrana/snapshots/`). So the columns arrive before the
  first row, and rule 1 of D18 applies.
- The server builds the whole pipeline response before it sends it (the
  server source, `hrana/http/mod.rs`). A response larger than
  `SQLD_MAX_RESPONSE_SIZE`, 10MB by default, fails with the code
  `RESPONSE_TOO_LARGE`. `SQLD_MAX_TOTAL_RESPONSE_SIZE`, 32MB by default, is a
  limit on all responses (the server source, `main.rs` and
  `hrana/result_builder.rs`). So a large result on `/v2/pipeline` is an
  error, and not a result cut short (D21).
- `POST /v3/cursor` takes `{"baton": ..., "batch": ...}` and streams its
  response as lines of JSON. The first line is `{"baton": ...,
  "base_url": ...}`. Each later line is an entry: `step_begin` with the
  columns, `row` with one row, `step_end` with `affected_row_count` and
  `last_insert_rowid`, `step_error`, or `error` (the Hrana spec). The code of
  the cursor reads no limit on the size, and it hands each entry to a
  channel that holds one entry (the server source, `hrana/cursor.rs`). So
  the cursor appears to have no cap, and to wait for the client.
- `affected_row_count` means something only after INSERT, UPDATE or DELETE.
  `last_insert_rowid` is a 64-bit integer as a string, or null (the Hrana
  spec).
- `POST /` returns an array with one `{"results": {"columns": [...],
  "rows": [...], ...}}` for each statement. `columns` holds only the names
  (the HTTP API document).
- Whether the server compresses a response is not measured.

## Types

Step 10 writes the type table. A value in Hrana is one of these (the Hrana
spec):

- `{"type": "null"}`: NULL.
- `{"type": "integer", "value": "42"}`: a 64-bit signed integer, as a string
  in JSON.
- `{"type": "float", "value": 1.5}`: a 64-bit float, as a JSON number.
- `{"type": "text", "value": "..."}`: a string in UTF-8.
- `{"type": "blob", "base64": "..."}`: bytes in base64. The server writes
  base64 with no padding, and reads it with or without padding (the server
  source, `libsql-hrana/src/proto.rs`).

Each value carries its own type, so one column can hold an integer in one
row and text in the next (the Hrana spec). `decltype` is the type that the
table declares for a column, and it is null for an expression (the Hrana
spec). Hrana has no type for a decimal, a time, a boolean or a UUID, so each
one arrives as one of the five types above.

JSON has no form for an infinite float. Step 6 measures how one arrives.

`POST /` writes an integer as a JSON number, a float as a JSON number, and a
blob as `{"base64": ...}` (the HTTP API document and the server source,
`http/user/mod.rs`). So an integer there passes through a JSON number (D19).

## Parameters

- `args` binds by position, and `named_args`, a list of `{"name": ...,
  "value": ...}`, binds by name. A name includes its prefix, `:`, `@` or
  `$`. If a name has no prefix, the server guesses it (the Hrana spec).
- A statement with an argument that it does not expect, or without one that
  it expects, is an error (the Hrana spec). A server can refuse positional
  and named arguments together, with the code
  `ARGS_BOTH_POSITIONAL_AND_NAMED` (the Hrana spec and the server source,
  `hrana/stmt.rs`).
- `describe` names each parameter, with `?NNN`, `:AAA`, `@AAA` and `$AAA`
  as written, and null for a bare `?` (the Hrana spec).
- So the server binds arguments, and the parser of D34 is not needed for
  binding.

## Transactions

- A stream is one SQLite connection, so `BEGIN`, the statements and
  `COMMIT` on one stream are one transaction across several HTTP requests,
  with the baton (the Hrana spec). `get_autocommit` and the condition
  `is_autocommit` say whether a transaction is open.
- A stream with no request for 10 seconds expires (the server source,
  `EXPIRATION` in `hrana/http/stream.rs`, and the Turso documents). The next
  request with its baton gets `STREAM_EXPIRED`. What happens to the open
  transaction is not measured.
- A write transaction that holds the lock for 5 seconds is rolled back when
  another writer waits for the lock (the server source, `TXN_TIMEOUT` in
  `connection/mod.rs` and `connection/connection_manager.rs`). The Turso
  documents give a transaction 5 seconds. The codes are
  `TRANSACTION_TIMEOUT` and `TRANSACTION_BUSY`.
- `POST /` refuses `BEGIN`, `COMMIT` and savepoints, and runs each request in
  one transaction (the HTTP API document and the server source,
  `http/user/mod.rs`).
- A driver never fakes a transaction (D20). Step 9 decides how a transaction
  lives with the two limits above.

## Errors

- An error of a statement in a pipeline is HTTP 200, with a result
  `{"type": "error", "error": {"message": ..., "code": ...}}` (the Hrana
  spec). An error of one step of a batch is in `step_errors` of the batch
  result, also with HTTP 200 (the Hrana spec).
- On `/v3/cursor`, `step_error` can come after `step_begin` and some rows,
  and `error` can come at any time as the last entry (the Hrana spec). So an
  error can arrive after some rows.
- An expired stream is HTTP 400 with
  `{"message":"The stream has expired due to inactivity","code":"STREAM_EXPIRED"}`
  (the server source, `hrana/http/mod.rs`, and the snapshot
  `tests__hrana__batch__server_timeout.snap`).
- A fault of the protocol, such as a baton that is not valid or a body that
  is not JSON, is HTTP 400 with `Content-Type: text/plain` and a message
  such as `Received an invalid baton` (the server source and the snapshot
  `tests__hrana__batch__server_restart_query_execute_invalid_baton.snap`).
- Every other error of the server is `{"error": "..."}` with its own status
  (the server source, `error.rs`). A failed authentication is 401, a refusal
  is 403, a namespace that does not exist is 404, and a busy transaction, too
  many requests or a slow creation of a database is 429. The status 503 means
  that the replicator or the store of namespaces stopped.
- The Hrana spec says that a body of an HTTP error can also be plain text or
  HTML from another part of the HTTP stack. It says that the codes are not
  stable yet.
- The codes of Hrana are `SQL_PARSE_ERROR`, `SQL_NO_STATEMENT`,
  `SQL_MANY_STATEMENTS`, `ARGS_INVALID`, `ARGS_BOTH_POSITIONAL_AND_NAMED`,
  `TRANSACTION_TIMEOUT`, `TRANSACTION_BUSY`, `SQL_INPUT_ERROR`, `BLOCKED`,
  `RESPONSE_TOO_LARGE`, `PROXY_ERROR`, `SQL_STORE_TOO_MANY` and
  `SQL_STORE_TOO_LARGE`. An error of SQLite has the code of SQLite, such as
  `SQLITE_CONSTRAINT` or `SQLITE_BUSY` (the server source, `hrana/stmt.rs`,
  `hrana/batch.rs` and `hrana/http/request.rs`).
- The server looks up the stream before it runs a statement (the server
  source, `hrana/http/mod.rs`). So `STREAM_EXPIRED` and a baton that is not
  valid appear to mean that no statement of the request ran. That is not
  measured.

## Cancellation and timeouts

- Hrana has no request that cancels a running statement (the Hrana spec).
- No code in the server interrupts a statement of a pipeline when the client
  disconnects. This session searched `libsql-server/src` for `interrupt`
  and found only the signal handler and the code `SQLITE_INTERRUPT` (the
  server source). Whether the statement stops is not measured.
- On `/v3/cursor`, the server ignores the error when it cannot hand an entry
  to a client that left (the server source, `emit_entry` in
  `hrana/cursor.rs`). So the statement appears to run to its end after the
  client leaves. That is not measured.
- The server closes a stream after 10 seconds with no request, because it
  cannot learn that an HTTP client left (the Hrana spec and the server
  source).
- `sqld` limits connections and requests with
  `SQLD_MAX_CONCURRENT_CONNECTIONS` and `SQLD_MAX_CONCURRENT_REQUESTS`, 128
  each by default (the server source, `main.rs`).

## Statements

- The text of a statement holds one statement. More than one is an error
  with the code `SQL_MANY_STATEMENTS` (the Hrana spec and the server source,
  `hrana/stmt.rs`).
- `sequence` runs several statements that semicolons separate, and returns
  no rows. `batch` runs several statements, each with its own result (the
  Hrana spec).
- `POST /` refuses a string with more than one statement (the server source,
  `http/user/mod.rs`).
- How the server treats a comment is not measured.

## Principals

- With HTTP basic, `sqld` has one user with full access (the Docker document
  and the dbmeta entry).
- With a JWT, the claim `a` gives the access, `ro` to read, `rw` to read and
  write, or `roa` to read an attached database. The claim `p` gives the
  access for each namespace, and `exp` gives the expiry (the server source,
  `auth/permission.rs` and `auth/user_auth_strategies/jwt.rs`). A token with
  `ro` has no right to write (the server source, `auth/permission.rs`).
- The dbmeta entry uses basic, so it has no ordinary user. It says that a
  principal with fewer rights needs a signed JWT, and a key pair that a test
  holds.
- Turso Cloud makes a token with `turso db tokens create`, and the flag
  `--read-only` makes a token that only reads. `--expiration` sets its life
  (the Turso documents).
- `GET /version` returns plain text in the form `sqld <version> (<first 8
  characters of the git commit> <build date>)` (the server source,
  `version.rs`). Whether it needs authentication is not measured.
- `usql` runs `SELECT sqlite_version()` for the version of SQLite
  (`usql/drivers/sqlite3/sqlite3.go` and
  `usql/drivers/moderncsqlite/moderncsqlite.go`). What each principal gets
  from it on `sqld` is not measured.

## Flavors

- `sqld`, the image `ghcr.io/tursodatabase/libsql-server`. The Docker
  document runs it with `-p 8080:8080`, and `SQLD_HTTP_LISTEN_ADDR` is
  `0.0.0.0:8080` in the image. The program itself listens on
  `127.0.0.1:8080` by default (the server source, `main.rs`). The data is in
  `/var/lib/sqld`. The release tags run from `v0.22.22` to `v0.24.33`. The
  tag `latest` is built from `main` and is not a release (ghcr.io and the
  dbmeta entry). The Docker document names the tag `latest-arm` for arm64.
  The dbmeta entry uses the port 8080 and `v0.24.33`, with basic
  authentication, and learns that the server is ready from
  `POST /v2/pipeline`.
- Turso Cloud. It speaks `POST /v2/pipeline` and has `GET /version` (the
  Turso documents). It fails R.
- Turso Database. Its program starts with `--sync-server <address>`, and it
  serves `POST /v2/pipeline` and `POST /db/{name}/v2/pipeline` in JSON, with
  `POST /pull-updates` for its sync (the Turso source). It has no route for
  `GET`, so it has no `/version`. This session found no code that reads the
  `Authorization` header. It returns the baton of the request in its
  response (the Turso source, `cli/sync_server.rs`). No image of it was
  looked for.
- `sqld` answers `GET /version` with a text that starts with `sqld`, so a
  driver can tell it apart from a flavor that answers otherwise. Step 9
  decides how the driver tells the flavors apart (DRIVER.md, "Flavors").

## Interfaces

Step 10 writes the interface table from the code.

## Faults

These are faults of the Go client, which a driver here does not repeat (the
Go client). The file is `libsql/internal/http/hranaV2/hranaV2.go` unless a
line names another.

- It sends every request through `http.DefaultClient`, which is global
  state.
- It reads each response whole with `io.ReadAll`, and decodes it with
  `json.Unmarshal` of `encoding/json`. The rows stay in memory, and
  `Rows.Close` does nothing (`shared/rows.go`). A driver here decodes one
  token at a time (D25).
- `Commit` and `Rollback` run with `context.Background`, so the context of
  the caller does not stop them.
- `Close` and `ResetSession` send `close` from a new goroutine, with
  `context.Background`, and nothing waits for it. So a goroutine runs after
  the connection closes.
- An integer that does not parse, and a blob whose base64 does not decode,
  become nil. So an error of decoding reaches the caller as NULL
  (`hrana/value.go`).
- A text value in a column whose `decltype` is `TIMESTAMP` or `DATETIME`
  becomes a `time.Time` in UTC if one of its layouts parses it, and stays a
  string if none does. So one column can give two Go types
  (`hrana/value.go`).
- The error of a statement becomes `errors.New` of the message, so the code,
  such as `SQLITE_CONSTRAINT`, is lost, and nothing wraps it with `%w`.
  Other messages start with `failed to execute SQL:` and hold the text of
  the SQL.
- It returns `driver.ErrBadConn` for `STREAM_EXPIRED`. Inside a transaction,
  the stream that held the transaction is then gone.
- A query of more than 20MB with no arguments is split on the client into
  batches of 4096 statements, inside a `BEGIN` and a `COMMIT` that the
  client adds. The client drops each statement of the text that starts with
  `begin`, `commit`, `end` or `rollback`.
- Several statements in one query go as one batch, and the client drops the
  last step from the results unless `WithSchemaDb` is set.
- It gives no column types. Its rows have no `ColumnType` method
  (`shared/rows.go`).
- `BeginTx` refuses a read only transaction and every isolation level but the
  default.
- Its DSN has two forms. `Driver.Open` takes the token in the query, and
  `NewConnector` refuses it there (`sql.go`). Neither form has `libsql` as
  its only scheme (D35).
- A `file:` URL opens another driver, `sqlite` or `sqlite3`, if one is
  registered (`sql.go`). Hard rule 2 names a `usql` driver that borrowed
  another driver.
- It depends on `github.com/antlr4-go/antlr/v4`, `github.com/coder/websocket`,
  `golang.org/x/sync` and `golang.org/x/exp` (`go.mod`). It uses the ANTLR
  parser to split statements and count parameters. D13 allows none of them.
- Its README says that the repository is deprecated. It names
  `github.com/tursodatabase/go-libsql` for Turso Cloud, which needs cgo (its
  README), and `github.com/tursodatabase/turso-go` for Turso Database, which
  is archived and calls a library through `purego` (GitHub and its README).

These are true of the Go client, and a driver here keeps them:

- It passes the context of the caller to `http.NewRequestWithContext` for a
  statement.
- A NULL arrives as nil.
- It keeps the order of the columns. It sorts the arguments by their
  ordinal, and not the columns (`shared/statement.go`).

## Second opinions

Step 7 fills this section. For step 3, DeepSeek answered two short prompts
on 2026-09-27. Gemini was asked the same questions, through
`gemini-3.8-flash` and `gemini-3.1-pro`, and every call closed the
connection or timed out. So DeepSeek is the only model that answered, and
step 5a and step 7 need a second model.

What DeepSeek said, and what the sources say:

- DeepSeek said that the Hrana protocol has a `cancel` request. The Hrana
  spec lists no such request.
- DeepSeek said that an idle stream lives 10 seconds, set by
  `--hrana-stream-timeout`. The server source agrees on 10 seconds, as the
  constant `EXPIRATION`, and has no such flag in `main.rs`.
- DeepSeek said that an expired stream rolls back its transaction. That is
  not measured.
- DeepSeek said that a pipeline response is capped at 100MB, and that
  `/v3/cursor` avoids the cap. The server source gives 10MB by default. It
  agrees that the cursor reads no limit.
- DeepSeek said that the claim `a` of a JWT is `ro` or `rw`. The server
  source agrees, and adds `roa`.
- DeepSeek said that Turso Database has a server that speaks Hrana over
  WebSocket and HTTP. The Turso source shows `POST /v2/pipeline` over HTTP.
  This session found no WebSocket in it.
- DeepSeek said that `GET /version` returns a version such as `0.24.0`. The
  server source returns `sqld` and the version, the commit and the date.
- DeepSeek said that the Go client has no `LastInsertId`, no `RowsAffected`,
  no named arguments and no `BeginTx`. The Go client has each of them.

## Open questions

None of these is in [PLAN.md](PLAN.md) yet. Each one waits for Ken.

- None about the name. Ken decided on 2026-09-28 that libSQL and Turso are
  one product with one driver, named `libsql`, and that `turso` is an alias
  in `dburl` (D76).
- Whether `dbrun` runs `sqld` with a JWT key, so that the tests have an
  ordinary user with `ro`, and the administrator with `rw`. The dbmeta
  entry has one user with basic authentication. This is a question for step
  4 and the `dbmeta` session.
- Whether Turso Database, through `--sync-server`, is a flavor that `dbrun`
  starts. It is in beta, and it has no `/version`.
- Whether the driver reads `/v2/pipeline`, which the server builds whole and
  caps at 10MB, or `/v3/cursor`, which streams. This is a decision of step 9
  (D21, D25 and D36).
- How a transaction lives with a stream that expires after 10 seconds and a
  write lock that ends after 5 seconds. This is a decision of step 9 (D20).
- How the DSN names the token or the user, the namespace and TLS. This is a
  decision of step 9 (D27 and D35).
- Whether the driver follows `base_url`, and whether it sends the
  credentials there. This is item 10 of step 9.
- None about the range of releases. libSQL has one line, so the range is
  one release, 0.24.33, which has an image and no GitHub release
  (dbmeta D112).
