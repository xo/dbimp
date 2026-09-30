# libSQL

This file holds what is known about libSQL and Turso, for the driver
`libsql`, which D73 places eighth after Neo4j. Its work item is W23. The
headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 0.24.33 on 2026-10-01, as `admin`, whose JWT can read
and write, and as `dbmeta_user`, whose JWT can only read. A fact marked
"recorded" is in `testdata/libsql/`, and the name in quotes after it is the
name of its request in `requests.json` there. A fact marked "measured" names
how it was measured. Each section starts with the measured facts. The facts
after them come from the sources of step 3, and each one that is not
measured says so. The sources, each read on 2026-09-27, are these:

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
- "The TypeScript client" is `github.com/tursodatabase/libsql-client-ts` at
  commit `9496c78` of 2026-09-02, version 0.18.0, read on 2026-10-01.
- "The Turso documents" are `docs.turso.tech/sdk/http/reference` and
  `docs.turso.tech/cli/db/tokens/create`.
- "The Turso source" is `cli/app.rs` and `cli/sync_server.rs` in
  `github.com/tursodatabase/turso`.
- "GitHub" is the releases and the tags of `tursodatabase/libsql`, and
  "ghcr.io" is the tag list of the image `ghcr.io/tursodatabase/libsql-server`.
- "The dbmeta entry" is `container/libsql.go` in `dbmeta`, as dbmeta D153
  changed it on 2026-10-01. It is staged, and not committed.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-09-27 and again on 2026-10-01.

## Summary

- libSQL is a fork of SQLite by Turso, under the MIT licence (the libSQL
  README). `libsql-server`, whose program is `sqld`, serves a libSQL
  database over HTTP and WebSocket (the libSQL README and the server source).
- 0.24.33 answers `GET /version` with `sqld 0.24.33 (40a151bd 2025-12-19)`,
  and runs SQLite 3.45.1 (recorded: "the version" and "the version of
  SQLite").
- Turso Cloud is a service that runs `sqld` for its customers, with the same
  HTTP API (the Turso documents). It is only a cloud service, so it fails R
  by step 2 of DRIVER.md.
- Turso Database is a new database by the same team, which rewrites SQLite
  in Rust. The libSQL README says that libSQL is maintained, and that new
  features go into Turso Database. Turso Database is in beta (GitHub). See
  Flavors.
- R holds. `dbrun` starts `libsql-0.24.33`, in the Staged tier with the
  cadence `tested` (measured with `dbrun list --json all` on 2026-10-01). So
  step 14 runs it. libSQL has one line, so the range is one release, which
  has an image and no GitHub release (dbmeta D112).
- H and S hold. `POST /v2/pipeline` with `SELECT sqlite_version()` answered
  HTTP 200 as both principals (recorded: "the version of SQLite").
  [TARGETS.md](TARGETS.md) places libSQL in P1.
- `dburl` has no scheme for `libsql`, `turso` or `sqld`
  (`dburl/scheme.go`, read on 2026-10-01). D76 names one driver, `libsql`,
  with `turso` as an alias in `dburl`.
- `usql` has no driver for libSQL (`usql/drivers/`). `usql/docs/BACKLOG.md`
  names the Go client as a driver that `usql` can add. So a driver here adds
  a database to `usql`, and replaces no driver (D24).
- `dbmeta` has no model for libSQL. Its model for `sqlite3` reads
  `sqlite_schema` and the table valued pragmas, which answer here (recorded:
  "schema: the table valued pragmas").
- The dbmeta entry starts `sqld` with `SQLD_AUTH_JWT_KEY`, an Ed25519 public
  key, and prints two principals, each with a fixed JWT as the password of
  its URL: `admin` with the claim `a` of `rw`, and `dbmeta_user` with `ro`
  (the dbmeta entry, and measured with `dbrun dsn --json` on 2026-10-01).
  Ken decided on 2026-10-01 that the entry has an ordinary user through a
  JWT (W23).

## Requests

These facts were recorded as both principals:

- `POST /v2/pipeline` and `POST /v3/pipeline` take
  `{"baton": ..., "requests": [...]}`, and answer the same form (recorded:
  "a statement on v2" and "a statement on v3").
- `POST /v3/cursor` takes `{"baton": ..., "batch": {"steps": [...]}}`, and
  answers lines of JSON: `{"baton": ..., "base_url": ...}`, then
  `step_begin` with the columns, a `row` line for each row, `step_end` with
  the counts, and `replication_index` (recorded: "a statement on the
  cursor"). Each step of the batch gets its own `step_begin` and `step_end`
  (recorded: "feature: a cursor over two steps").
- A cursor leaves its stream open, and answers with a baton that is not
  null (recorded: "a statement on the cursor"). A pipeline whose last
  request is `close` answers with a baton of null, and one with no `close`
  answers with a baton that a later request can use (recorded: "a pipeline
  with no close" and "close the stream of the pipeline").
- `POST /v1/execute` takes `{"stmt": ...}` and answers `{"result": ...}`.
  `POST /` takes `{"statements": [...]}` and answers an array of
  `{"results": {"columns": [...], "rows": [...]}}`, with each value as plain
  JSON (recorded: "a statement on v1" and "a statement on the first API").
- `GET /v2` and `GET /v3` each answer `Hello, this is HTTP API v2 (Hrana
  over HTTP)`, and `GET /health` answers HTTP 200 with no body (recorded).
  `GET /version` needs no credentials (measured with `curl` on 2026-10-01).
- A request for another namespace with the header `x-namespace` ran on the
  default database, because this server runs without namespaces (recorded:
  "feature: another namespace").
- A request with `Accept-Encoding: gzip` gets a body in gzip, with
  `Content-Encoding: gzip` (recorded: "a gzip answer").
- The token goes in `Authorization: Bearer`. A token with a wrong signature
  gets HTTP 401 with `{"error":"Unauthorized: `The JWT is invalid`"}`
  (recorded: "a wrong password").

These facts come from the sources, and are not measured:

- Each request of a pipeline has a `type`: `execute`, `batch`, `sequence`,
  `describe`, `store_sql`, `close_sql`, `get_autocommit` or `close` (the
  Hrana spec). A statement is `{"sql": ..., "sql_id": ..., "args": [...],
  "named_args": [...], "want_rows": ...}`.
- A stream is one SQLite connection on the server. The baton holds the
  number of the stream and a sequence number, signed by the server, and a
  baton sent twice is refused (the Hrana spec and the server source,
  `hrana/http/stream.rs`).
- A response can hold `base_url`, and then the next requests of the stream
  go there. `sqld` sets it from `SQLD_HTTP_SELF_URL` (the server source).
  Every recorded answer has a `base_url` of null.
- The server reads the namespace from the header `x-namespace`, or from the
  first label of the `Host` header, when it runs with namespaces (the server
  source, `http/user/db_factory.rs`).
- Authentication is basic from `SQLD_HTTP_AUTH`, a JWT that the server tests
  with an Ed25519 key, or none. With `SQLD_HTTP_AUTH` set, the server ignores
  the key (the server source, `main.rs`). Turso Cloud takes a token in
  `Authorization: Bearer` (the Turso documents).

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has no scheme yet.
- The Go client registers `libsql`. It takes `libsql://`, `https://`,
  `http://`, `wss://`, `ws://` and `file:` URLs, and reads a token from the
  query keys `auth_token`, `authToken` or `jwt` (the Go client, `sql.go`).
- A database in Turso Cloud has the URL
  `https://[databaseName]-[organizationSlug].turso.io` (the Turso
  documents).
- The dbmeta entry writes `http://admin:<token>@127.0.0.1:<port>` and
  `http://dbmeta_user:<token>@127.0.0.1:<port>`. The name of the user is a
  label, and the server reads only the token.

## Responses

These facts were recorded as both principals:

- A pipeline answer is `{"baton": ..., "base_url": ..., "results": [...]}`,
  with `baton` first. Each result is `{"type": "ok", "response": ...}` or
  `{"type": "error", "error": {"message": ..., "code": ...}}` (recorded:
  "a statement on v2").
- A statement result holds `cols`, then `rows`, then `affected_row_count`,
  `last_insert_rowid`, `replication_index`, `rows_read`, `rows_written` and
  `query_duration_ms` (recorded). Each column is `{"name": ...,
  "decltype": ...}`, in the order of the statement, and two columns can have
  one name (recorded: "columns in the order of the statement" and "two
  columns with one name"). So the columns arrive before the first row, and
  rule 1 of D18 applies. On the cursor, `step_begin` names the columns
  before the first `row` (recorded).
- `decltype` is the declared type of a column of a table, such as `INTEGER`,
  `F32_BLOB(3)` or null for a column with no type, and null for an
  expression (recorded: "the declared types").
- A statement that returns no rows has `"rows": []` (recorded: "no rows").
- `affected_row_count` is the count of an `INSERT`, `UPDATE` or `DELETE`,
  and 0 for DDL and a `SELECT`. `last_insert_rowid` is a string, or null
  (recorded: "crud: insert" and "crud: update"). An upsert that updates gave
  `"last_insert_rowid":"0"` (recorded: "crud: upsert").
- A pipeline answer of 20000 rows arrived whole. One of 500000 rows gave the
  error `RESPONSE_TOO_LARGE`, "Response is too large", in HTTP 200 (recorded:
  "a result of 20000 rows" and "a result of 500000 rows").
- The cursor sent the 500000 rows, 58389239 bytes, with no cap (measured with
  `curl` on 2026-10-01).

These facts come from the sources, and are not measured:

- The server builds the whole pipeline answer before it sends it, and
  `SQLD_MAX_RESPONSE_SIZE` is 10MB by default (the server source, `main.rs`
  and `hrana/result_builder.rs`). The cursor hands each entry to a channel
  that holds one entry (the server source, `hrana/cursor.rs`).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table,
Ken reviewed it on 2026-10-01 (D147), and step 10 will write it from the
code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| INTEGER | integer | `int64, for each declared type whose name holds INT, such as BIGINT` | `int64` | `INTEGER` | yes |
| REAL | float | `float64, for each declared type whose name holds REAL, FLOA or DOUB. An infinity is +Inf, because the server loses its sign` | `float64` | `REAL` | yes |
| TEXT | string | `string, for each declared type whose name holds CHAR, CLOB or TEXT` | `string` | `TEXT` | yes |
| BLOB | binary | `[]byte, from base64` | `[]uint8` | `BLOB` | yes |
| NUMERIC | number | `int64 for an integer, and float64 for a float, for each declared type that no other row names, such as DECIMAL(10,2)` | `interface {}` | `NUMERIC` | yes |
| BOOLEAN | boolean | `bool, from an integer: 0 is false, and any other is true` | `bool` | `BOOLEAN` | yes |
| DATE | date | `dbimp.Date, from text of the form YYYY-MM-DD` | `dbimp.Date` | `DATE` | yes |
| DATETIME | local timestamp | `dbimp.LocalDateTime, from text with no zone` | `dbimp.LocalDateTime` | `DATETIME` | yes |
| TIMESTAMP | timestamp | `time.Time, from RFC 3339 text, or text with no zone in UTC` | `time.Time` | `TIMESTAMP` | yes |
| F32_BLOB | vector | `dbimp.Vector[float32], from the bytes of the BLOB, and []byte for a BLOB that is not 4n bytes` | `dbimp.Vector[float32]` | `F32_BLOB` | yes |
<!-- /dbimp:types -->

The table follows D140, which Ken decided for rqlite: the Go type follows
the rules of affinity of SQLite for the declared type, and a value whose
form cannot have the Go type of its column keeps the Go type of its own
storage class. Hrana names the storage class of each value, so that
fallback is exact here. `ANY` and a column with no declared type read by the
storage class of each value, with the scan type `interface {}` (D140).

These facts were recorded as both principals, from a table with one column
of each declared type (recorded: "every type" and "every type with the
storage class"):

- Each value arrives with its storage class. NULL is `{"type": "null"}`, an
  integer is `{"type": "integer", "value": "42"}` with the value as a
  string, a float is `{"type": "float", "value": 1.5}`, text is
  `{"type": "text", "value": ...}`, and a BLOB is `{"type": "blob",
  "base64": ...}` in base64 with no padding.
- An integer keeps every digit from -9223372036854775808 to
  9223372036854775807. A literal beyond that range is a float (recorded: "an
  integer beyond the range").
- The float 1.0 arrives as `1.0`, with the type `float` (recorded: "literals
  of each class"). So a float never looks like an integer.
- A value keeps the storage class that SQLite stored it with. A `REAL`
  column held the text `'abc'`, and an `INTEGER` column the float 1.5.
- The server converts nothing. A `BOOLEAN` column holds the integers 1, 0
  and 2. A `DATE` column holds the text `'2026-10-01'` and `'not a date'`,
  and the integer 42. A `TIMESTAMP` column holds the text
  `'2026-10-01T12:34:56.123456789+05:30'` and the float 1.5.
- A BLOB of an expression, such as `SELECT x'80ff'`, arrives as a BLOB with
  every byte (recorded: "a blob that is not text").
- An infinity arrives as `{"type": "float", "value": null}`, with its sign
  lost (recorded: "an infinity" and "a negative infinity").
  `CAST(1e999 AS TEXT)` gives `Inf`, and `CAST(-1e999 AS TEXT)` gives `-Inf`
  (measured with `curl` on 2026-10-01). `0.0 / 0` is NULL (recorded: "a
  NaN").
- A column `F32_BLOB(3)` holds a BLOB of three float32 in little-endian
  order. `vector32('[1,2,3]')` is the 12 bytes `AACAPwAAAEAAAEBA` in base64,
  and `vector_extract` gives the text `[1,2,3]` (recorded: "a vector").
- A `STRICT` table refuses a value of the wrong storage class with
  `SQLITE_CONSTRAINT` (recorded: "a wrong type in the strict table").
- `POST /` writes an integer as a JSON number, and 12345678901234567890 as
  `1.2345678901234567e19`, and a BLOB as `{"base64": ...}` (recorded: "every
  type on the first API").

## Parameters

These facts were recorded as both principals:

- `args` binds `?` by position, and `?NNN` by its number (recorded:
  "positional parameters" and "a numbered parameter"). `named_args` binds
  `:name`, `@name` and `$name`, with the prefix or without it (recorded:
  "named parameters" and "named parameters with no prefix").
- Each argument carries its storage class. The float 1.0 binds as a `real`
  (recorded: "positional parameters").
- An integer must be a string. The integer as a JSON number, and a string
  beyond the range of `int64`, fail the whole request with HTTP 400 and
  `Cannot deserialize client message from JSON` (recorded: "an integer as a
  number" and "an integer beyond the range").
- A float must be a JSON number. The string `"Infinity"` fails the whole
  request with HTTP 400 (recorded: "a float parameter that is infinite").
- A text of the form `x'41'` binds as text (recorded: "a text that looks
  like hex").
- Too few or too many arguments fail with `ARGS_INVALID`. Positional and
  named arguments together fail with `ARGS_BOTH_POSITIONAL_AND_NAMED`
  (recorded: "too few parameters", "too many parameters" and "a positional
  and a named parameter").
- A `?` in a string or a comment is not a parameter (recorded: "a parameter
  in a string and a comment").
- `describe` names each parameter, with null for a bare `?`, and the
  columns, and says whether the statement is read-only (recorded: "describe
  a statement").

## Transactions

These facts were recorded as the administrator:

- A `BEGIN` on a stream stays open across requests that send its baton. A
  read on another stream did not see the row of the transaction, a read on
  its stream did, and a `ROLLBACK` removed it (recorded: "insert in the
  transaction", "read outside the transaction", "read in the transaction"
  and "read after the rollback"). `COMMIT` keeps the row (recorded: "read
  after the commit").
- `get_autocommit` on `/v3/pipeline` gives `false` inside a transaction
  (recorded: "the autocommit state in the transaction").
- A stream with no request for 12 seconds had expired. The request with its
  baton got HTTP 400 with `{"message":"The stream has expired due to
  inactivity","code":"STREAM_EXPIRED"}`, and the insert of its open
  transaction was gone (recorded: "a statement after the stream expired" and
  "read after the stream expired").
- `BEGIN TRANSACTION READONLY`, which the TypeScript client sends, took an
  `INSERT` with no error. So the server does not keep a transaction
  read-only (recorded: "a read-only transaction").
- A batch with conditions ran `BEGIN`, failed its `INSERT`, skipped the
  `COMMIT` that its condition named, and ran the `ROLLBACK` (recorded: "a
  batch with conditions").

These facts come from the sources, and are not measured:

- A write transaction that holds the lock for 5 seconds is rolled back when
  another writer waits for the lock, with the codes `TRANSACTION_TIMEOUT`
  and `TRANSACTION_BUSY` (the server source, `connection/mod.rs`).
- A stream expires after 10 seconds with no request (the server source,
  `EXPIRATION` in `hrana/http/stream.rs`, and the Turso documents).

## Errors

These facts were recorded as both principals:

- An error of a statement is HTTP 200 with a result of the type `error`,
  such as `SQL_PARSE_ERROR` for a syntax error, and `SQLITE_UNKNOWN` with
  `SQLite error: no such table: dbimp_nosuch` (recorded: "a syntax error"
  and "an unknown table"). A constraint is `SQLITE_CONSTRAINT` (recorded:
  "schema: unique constraint enforced").
- On a pipeline, an error while a statement makes its rows gives the error
  and no rows (recorded: "an error inside the rows"). On the cursor, the
  rows before it arrive, then `step_error` (recorded: "an error inside the
  rows on the cursor"). So an error can arrive after some rows on the
  cursor.
- The requests of a pipeline after a failed one still run (recorded: "an
  error and then a statement").
- A fault of the protocol is HTTP 400 with plain text: `Cannot deserialize
  client message from JSON` for a body that is not JSON, and `Received an
  invalid baton` (recorded: "a body that is not JSON" and "a baton that is
  not valid"). An unknown path is HTTP 404 with no body (recorded).
- A token that can only read gets HTTP 403 with `{"error":"Authorization
  forbidden: Current session doesn't not have Write permission to namespace
  default"}` for a request that holds any write, a `PRAGMA` too, and no
  statement of it runs (recorded as `dbmeta_user`: "crud: insert" and
  "schema: the pragma of foreign keys").

## Cancellation and timeouts

- A statement stops when the client disconnects, on `/v2/pipeline` and on
  `/v3/cursor`. A statement that runs for about 20 seconds used one second
  of processor time on the server when `curl` gave up after one second, and
  nothing in the six seconds after (measured from `/proc/<pid>/stat` of
  `sqld` on 2026-10-01). The next request answered at once (recorded: "a
  query after the client left"). The step 3 draft said the opposite, from the
  server source, and both models said the opposite on 2026-10-01.
- Hrana has no request that cancels a running statement (the Hrana spec).

## Statements

These facts were recorded as both principals:

- A text with two statements fails with `SQL_MANY_STATEMENTS`, and an empty
  one with `SQL_NO_STATEMENT` (recorded: "two statements in one text" and
  "an empty statement").
- A statement that ends with a semicolon runs, and a comment is part of the
  name of a column that it follows (recorded).
- `sequence` runs statements that semicolons separate, and returns no rows
  (recorded: "a sequence").
- Foreign keys are on. `PRAGMA foreign_keys` gives 1, and a broken reference
  fails with `SQLITE_CONSTRAINT` (recorded: "schema: the pragma of foreign
  keys" and "schema: a foreign key that is broken").
- `ALTER TABLE ... ALTER COLUMN` changes a column, and refuses a column with
  a `UNIQUE` constraint (recorded: "schema: alter a column with no
  constraint" and "schema: alter column").
- `ATTACH`, `CREATE FUNCTION ... LANGUAGE wasm` and `BEGIN CONCURRENT` fail
  with `SQL_PARSE_ERROR` (recorded: "feature: attach", "feature: a user
  function in WASM" and "feature: begin concurrent").
- A vector index and `vector_top_k` find the nearest row, and a table with
  `RANDOM ROWID` gives a random rowid (recorded: "feature: a vector index"
  and "feature: a random rowid").

## Principals

- `admin` runs every statement. `dbmeta_user` runs every read, and gets HTTP
  403 for every request that writes, so it cannot make a table (recorded).
- Both principals get the version from `GET /version` with no credentials,
  and from `SELECT sqlite_version()`, which gives the version of SQLite
  (recorded: "the version" and "the version of SQLite").
- With a JWT, the claim `a` gives the access: `ro`, `rw` or `roa`. The claim
  `p` gives the access for each namespace (the server source,
  `auth/permission.rs`). Turso Cloud makes a token with `turso db tokens
  create`, and `--read-only` makes one that only reads (the Turso
  documents).

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

`libsql/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1 on a new stream, which checks the token. |
| `driver.SessionResetter` | no | A connection holds a stream only while a transaction is open, and database/sql ends the transaction before it reuses the connection (D102). |
| `driver.Validator` | no | A connection holds nothing on the server outside a transaction, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, a uint64 and the civil types of the root package, which the driver writes itself (D152). |
| `driver.QueryerContext` | yes | A query reads its rows from /v3/cursor, one entry at a time (D149). |
| `driver.ExecerContext` | yes | Exec runs on /v3/pipeline, and gives affected_row_count and last_insert_rowid (D152). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx sends BEGIN on a stream, which the baton holds across requests. ReadOnly fails, because the server keeps no transaction read-only (D150). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A cursor holds one statement, so an answer has one result (D149). |
| `driver.RowsColumnTypeScanType` | yes | decltype names the declared type of each column, whose affinity gives the Go type (D147). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | decltype names the declared type of each column, such as BIGINT or F32_BLOB(3). |
| `driver.RowsColumnTypeLength` | no | decltype names the length as the statement wrote it, and SQLite does not keep to it. |
| `driver.RowsColumnTypeNullable` | yes | The answer does not name NOT NULL, and a column of SQLite holds NULL unless its table forbids it. |
| `driver.RowsColumnTypePrecisionScale` | no | decltype names the precision as the statement wrote it, and SQLite does not keep to it. |
<!-- /dbimp:interfaces -->

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

Step 7 tested each lead on the server. Gemini and DeepSeek were asked the
same questions on 2026-10-01. On 2026-09-27 only DeepSeek answered.

- Foreign keys. Gemini said that they are on by default, and DeepSeek that
  they are off. The server has them on. Gemini was right.
- A client that disconnects. Both said that the server does not stop the
  statement. The server stopped it. Both were wrong.
- An infinity. Gemini said that it arrives as a string or null, and DeepSeek
  was unsure. It arrives as a float with a value of null. Both suggested
  `CAST(x AS TEXT)`, which gives `Inf` and `-Inf`.
- `BEGIN TRANSACTION READONLY`. Both said that the server does not enforce
  it. The server took an `INSERT` in it.
- A token with `ro`. Both said that the whole request gets HTTP 403. The
  server agrees.
- A cursor. Both said that the stream expires if the client does not close
  it. The cursor answers with a baton, so the stream stays open.
- `ATTACH`, WASM functions and `BEGIN CONCURRENT`. Both said that `ATTACH`
  and WASM functions work, and DeepSeek named `BEGIN CONCURRENT`. The server
  refuses all three.
- Namespaces. Both named them. The server ignores `x-namespace`, because it
  runs without namespaces.
- On 2026-09-27, DeepSeek said that Hrana has a `cancel` request, that a
  pipeline answer is capped at 100MB, and that `GET /version` returns a bare
  version. The Hrana spec has no `cancel`, the cap is 10MB, and `/version`
  gives `sqld 0.24.33 (40a151bd 2025-12-19)`.

Step 8a asked both models on 2026-10-01 to review the mapping of the types:

- Both agreed that D140 applies, with the storage class of each value for
  the fallback, and that `BOOLEAN`, `DATE`, `DATETIME` and a vector map as
  the table says.
- DeepSeek said that a `TIMESTAMP` with no zone read as UTC is a guess, and
  that the driver keeps it as text or documents UTC. Gemini agreed with UTC,
  and Ken chose UTC (D147).
- They disagreed on an infinity. DeepSeek agreed that it is an error of the
  row, because nil would look like NULL. Gemini said that the driver returns
  `+Inf`, or an error of the column only. Ken chose `+Inf` (D147).
- DeepSeek said that the driver checks that a vector has 4n or 8n bytes.

## Open questions

- None of step 9. Ken decided D147 to D152 on 2026-10-01.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It
was written on 2026-10-01 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of libSQL from the sections above.

### The server

| | Couchbase | libSQL |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /v3/cursor` or `/v3/pipeline`, with a statement of Hrana, its typed arguments, and the baton of a stream (Requests) |
| Database | The key `query_context` of the body | The header `x-namespace`, on a server with namespaces |
| Language | SQL++, which is close to SQL | The SQL of SQLite, with vectors and `ALTER COLUMN` of libSQL |
| DDL | In SQL++ | In SQL |
| Parameters | `?`, `$1` and `$name` | `?`, `?NNN`, `:name`, `@name` and `$name`, bound by the server with a storage class each (Parameters) |
| Framing | One body for the whole result, which does not page | Lines of JSON on the cursor, one for each row, with no cap. A pipeline builds its answer whole, up to 10MB (Responses) |
| Columns | `signature`, before the first row | `step_begin` with `cols`, before the first row |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement |
| Errors | Can come with HTTP 200, after some rows | HTTP 200 with the error of a statement, or `step_error` after some rows on the cursor (Errors) |
| Types | JSON. No date, decimal, UUID or binary | The storage class of each value, with the integer in a string and a BLOB in base64. An infinity loses its sign (Types) |
| Cancel | The server stops a query when the client leaves | The server stops a statement when the client leaves (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | `BEGIN` on a stream, carried by its baton, which expires after 10 seconds with no request (Transactions) |
| Authentication | Basic, or `creds` in the body | A JWT as Bearer, or basic with `SQLD_HTTP_AUTH` |
| Default port | 8093, or 18093 with TLS | 443 with TLS. `sqld` listens on 8080 |

The differences that a caller sees:

- TLS is on by default, and a DSN without it names its port (D148).
- A query reads a cursor, which streams, and closes its stream after it
  (D149).
- A transaction lives on a stream that expires after 10 seconds with no
  statement, and the server then rolls it back (D150).
- A token that can only read gets HTTP 403 for any write, before the
  statement runs (Errors).

### The driver

| | `couchbase` | `libsql` |
| --- | --- | --- |
| Size, without tests, on 2026-10-01 | About 1300 lines in 8 files | About 1300 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `NoTLS`, `User`, `Password`, `Auth`, `Namespace` |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithNamespace`, through `WithOptions` or an argument (D109). `WithTimeout` ends the request, which stops the statement. `WithReadonly(true)` gives `dbimp.ErrNotSupported`. `WithDatabase` sets the namespace, as `WithNamespace` does. `WithParameter` sets any key of the statement of Hrana |
| Arguments | Sent to the server as `args` and `$name` | Sent to the server as typed values of Hrana, in `args` or `named_args`, never both (D152) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature. `SELECT RAW` has a reader of its own | A reader of the lines of the cursor, one entry at a time (`rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from `decltype`, by the rules of affinity of SQLite, and `ColumnTypeNullable`, which says that every column can be NULL (D147) |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the affinity of the column: `int64`, `float64`, `string`, `[]byte`, `bool`, `dbimp.Date`, `dbimp.LocalDateTime`, `time.Time` and `dbimp.Vector[float32]`. A value of another storage class keeps its own Go type (D140 and D147) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` from `affected_row_count`, and `LastInsertId` from `last_insert_rowid` (D152) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` sends `BEGIN` on a stream. `ReadOnly` and an isolation level give `dbimp.ErrNotSupported` (D150) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds a stream only while a transaction is open |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The request carries the context, and the server stops the statement when the client leaves (D152) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Message}`, which unwraps to `*dbimp.StatusError` for a status that is not 2xx, and `CodeStreamExpired` |
| Authentication | Basic | Bearer by default, or basic with `auth=basic` (D94 and D148) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `CodeStreamExpired`, `AuthBearer` and `AuthBasic` |

The differences that a caller sees:

- A value has the Go type of the affinity of its column, and a value of
  another storage class keeps its own Go type, where Couchbase gives JSON
  shapes (D147).
- An infinity reads as `+Inf`, because the server loses its sign (D147).
- A `[]byte` argument goes as a BLOB of Hrana, in base64 with no padding,
  where Couchbase sends base64 in a JSON string (D44 and D152).
- `WithReadonly(true)` and a read-only transaction fail, where Couchbase
  sends `readonly`, because the server keeps neither read-only (D150).
- `WithTimeout` ends the request, and sets no timeout on the server, because
  Hrana has none. The server stops the statement when the request ends. No
  decision names this yet.
