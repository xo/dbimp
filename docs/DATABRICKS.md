# Databricks

This file holds what is known about Databricks over its SQL Statement Execution API, for a
possible driver `databricks` (W39 and D188). The headings are the template of
[DRIVER.md](DRIVER.md). A fact is "recorded", with the name of its request in
quotes, "measured", with the date, or "not measured", with its source.

Step 6 recorded one login on 2026-10-10, as one run of 331 requests, on a Databricks
Free Edition workspace on AWS in the region us-east-2. The workspace has a Serverless
Starter Warehouse of size 2X-Small and type PRO, with the catalog `workspace` and
the schema `dbimp`. The login is a personal access token of one service principal.
The principal owns the schema `dbimp` and holds `USE CATALOG` on `workspace`
(recorded: "the warehouse", "the schemata"). The host and the warehouse id are
redacted in the files as `dbc-00000000-0000` and `1111222233334444`. The
recordings are under `testdata/databricks/`, and `testdata/databricks/requests.json` is
the script. The script drops every table that it makes, at its start and at its end.

A fact marked "source: documentation" comes from the Databricks documentation of the
Statement Execution API and of OAuth. This work made no network call, so the agent
wrote those facts from what it knows of the documentation and did not open it on
2026-10-10. Ken or a later pass must make sure of each one before a decision rests
on it.

The recordings give no fact about two things, and both are named where they matter.
The first is the body that a presigned link returns, because the recorder sends
every request with the token and never fetched a link. The second is a statement
that runs longer than 50 seconds, because every statement that the script sent
finished in about 5 seconds.

## Summary

- Product: Databricks, a hosted lakehouse. The service answers SQL on a SQL
  warehouse, and the engine is Spark SQL with its own additions. `SELECT version()`
  answers `4.2.0` and a 40 character build hash (recorded: "the version"). The
  release of the SQL warehouse channel is `2026.38` (recorded: "the history of
  queries", the member `channel_used`). There is no release to choose, because
  the service has one. The measured release is "databricks", and `dbrun` has
  no name for it (read of `dbmeta/container`, 2026-10-10).
- R: fails. Databricks has no emulator, `dbrun` has no entry for it, and a login
  needs a token of a workspace. `dbmeta/hosted/hosted.go` lists it as a hosted
  service with the form `databricks://token:<personal access token>@<workspace>.databricks.com:443/sql/1.0/endpoints/<warehouse id>`
  and no emulator (read, 2026-10-10). CI cannot test it without a secret.
  D188 says so.
- H: passes. `POST /api/2.0/sql/statements` takes JSON and answers JSON
  (recorded: "a statement"). The rows of the default form are JSON text, and no
  binary encoding is needed (recorded: "a statement with the format JSON_ARRAY and
  the disposition INLINE"). The API can also answer in the Arrow stream form, which
  only the disposition `EXTERNAL_LINKS` allows (recorded: "a statement with the
  format ARROW_STREAM and the disposition INLINE").
- S: passes. The dialect is Spark SQL as Databricks runs it, with `ANSI` mode on by
  default (recorded: "a division by zero"). It answered `SELECT`, `INSERT`, `UPDATE`,
  `DELETE`, `MERGE`, `TRUNCATE`, `CREATE TABLE`, `ALTER TABLE`, `CREATE VIEW` and
  SQL scripts (recorded: "a select of the rows", "an insert and its answer", "an update and its
  answer", "a delete and its answer", "a merge and its answer", "a truncate", "an alter table that
  adds a column", "a create view", "a script with a compound statement"). The priority is
  the request of Ken on 2026-10-10 (D188).
- Whether it can be a driver: yes, over HTTP and JSON with no Arrow. No condition
  of "When it cannot be a driver" failed on the recordings, and one is at risk.
  - The columns and their order come before the first row. The member `manifest`
    comes before the member `result` in the body, and it holds `schema.columns` in
    the order of the statement, with a `position` (recorded: "columns of a result with
    rows", "columns in the order of the statement"). A result with no rows still has
    the columns (recorded: "columns of a result with no rows"). A statement that
    returns nothing has `column_count` of 0 and no `columns` (recorded: "a statement
    with no result").
  - A result can be read one row at a time. An inline chunk is one JSON object
    whose member `data_array` holds one array for each row, so a decoder can read
    it token by token (recorded: "3000 rows of 300 characters", a body of 935384
    bytes with one chunk). The shape of the body behind a presigned link is not
    recorded.
  - A result is never cut with no sign when the driver sends no limit.
    `manifest.truncated` is `true` when `row_limit` or `byte_limit` cut it
    (recorded: "3000 rows with the row limit 10", "3000 rows with the byte limit
    1000"), and `false` for 3000 rows with no limit (recorded: "3000 rows of two
    narrow columns").
  - The condition at risk is D21 for a large result. Every result that the script
    made came in one chunk, up to 3000 rows and 3 MB (recorded: "3000 rows of 300
    characters", "3000 rows of 1000 characters as external links"), so no
    recording shows a result of more than one chunk, the member
    `next_chunk_index`, or what the server does with an inline result above its
    limit of 25 MiB. The request member `byte_limit` has the range 1 to 26214400,
    which is 25 MiB (recorded: "3 rows with the byte limit 0"). Documentation
    says that an inline result holds at most 25 MiB and a result of external links
    up to 100 GiB (not measured, source: documentation). A second live pass must
    send a result of more than 25 MiB with each disposition.
  - The package does not need Arrow, Thrift or the cloud fetch of Arrow (D13).
    A driver that wants a result above the inline limit needs the second kind of
    fetch, which is a `GET` to a presigned link of another host with no
    `Authorization` header (see Responses).
- What is lossy or hard in this form, as far as the recordings show:
  - Every value is text, and a driver must decode it by `type_name` and
    `type_text` (see Types).
  - A `TIMESTAMP` and a `TIMESTAMP_NTZ` keep only milliseconds in the text. The
    value `2024-01-02 03:04:05.123456` arrived as `2024-01-02T03:04:05.123Z`,
    also from a stored column (recorded: "the timestamp type", "the rows of every
    type"). The text of a link of `JSON_ARRAY` is not recorded, so whether it
    keeps the microseconds is not measured.
  - An `ARRAY`, a `MAP` and a `STRUCT` arrive as JSON text in one string, and every
    leaf is a string, such as `["1","2"]` for an array of `INT`. A driver needs the
    grammar of `type_text` to decode the leaves (recorded: "the array type", "nested
    complex types").
  - The session does not last. `SET`, `USE`, a temporary view and a declared variable do
    not reach the next request (see Transactions).
  - The first statement after the warehouse stopped takes about 15 seconds, and the
    request waits for it (recorded: "setup: warm the warehouse").
- Scheme in `dburl`: `Name` is `databricks`, the aliases are `br`, `brick`,
  `bricks` and `databrick`, the generator is `genSchemeSuffix("databricks",
  ".cloud.databricks.com", true)`, and the `Dialect` is `databricks`. The deployment
  is hosted. `GoPackage` is `github.com/xo/dbimp/databricks`. The generator rewrites
  the scheme, returns an error when the URL names no host, and adds
  `.cloud.databricks.com` only to a host that has no dot and no port (read of
  `dburl/scheme.go` and `dburl/dsn.go`, 2026-10-10). A host of Azure or of Google
  Cloud has dots, so it passes unchanged (read of `dburl/dburl_test.go`,
  2026-10-10).
- Driver of `usql` now: `github.com/databricks/databricks-sql-go` v1.16.0. It uses
  Thrift over HTTP and Arrow, and it has an optional backend of Rust bindings behind
  the build tag `databricks_kernel` (read of `usql/go.mod` and of the module,
  2026-10-10). `usql` registers it with an error hook only, so `usql` has no
  `Version` statement for it (read of `usql/drivers/databricks/databricks.go`,
  2026-10-10). `usql` lists its cost as 4.6, with its SDK and Arrow v12 as the cause
  (read of `usql/docs/BACKLOG.md`, 2026-10-10).
- What `dbmeta` has: the dialect `databricks` and the entry of the hosted
  service, with no container and no model (read of `dbmeta/dialect.go` and
  `dbmeta/hosted/hosted.go`, 2026-10-10).

## Requests

- A statement is `POST /api/2.0/sql/statements` with a JSON body, the header
  `Content-Type: application/json`, and the answer `Content-Type: application/json`
  (recorded: "a statement"). The path `/api/2.0/sql/statements/` with a trailing slash
  gave the same answer (recorded: "a request with a trailing slash"), and a query
  string was ignored (recorded: "a request with a query string").
- The body has these members, each one recorded:
  - `warehouse_id` is required. A body with none gets HTTP 400 with the code
    `BAD_REQUEST` (recorded: "a body with no warehouse", "a body that is an empty
    object", "an empty body").
  - `statement` is required (recorded: "a body with no statement"). An empty text is
    HTTP 200 and the state `FAILED` (recorded: "an empty statement").
  - `catalog` and `schema` set the namespace of the statement (recorded: "the
    catalog and the schema"). With neither, the namespace is the catalog
    `workspace` and the schema `default` (recorded: "a statement with no catalog
    and no schema"). A catalog or a schema that does not exist is not an error
    when the statement names no table, because `SELECT 1` ran (recorded: "a
    missing catalog", "a missing schema"). What `current_schema()` answers then is
    not measured.
  - `wait_timeout` is a text such as `"5s"`. It is `"0s"`, or from `"5s"` to `"50s"`. `"51s"`
    gets HTTP 400 with `INVALID_PARAMETER_VALUE` and the message "The wait_timeout
    field must be 0 seconds (disables wait), or between 5 seconds and 50 seconds."
    (recorded: "a wait timeout that is too long"). Text such as `"soon"` gets HTTP 400
    with "Error while parsing type: duration." (recorded: "a wait timeout that is
    text"). With no `wait_timeout` the call waited, and the answer was complete
    for `SELECT 1` (recorded: "a statement with no wait timeout"). The default value is
    not measured (source: documentation says 10 seconds).
  - `on_wait_timeout` is `CONTINUE` or `CANCEL`. The server ignores any other
    value (recorded: "an on_wait_timeout that is not known").
  - `row_limit` is an integer from 0 to 2147483647, and `byte_limit` is an integer from
    1 to 26214400 (recorded: "3 rows with the row limit that is negative", "3 rows with the
    byte limit 0"). Zero rows is not "no limit": `row_limit` 0 gave no row and
    `truncated` of `true` (recorded: "3000 rows with the row limit 0").
  - `disposition` is `INLINE` or `EXTERNAL_LINKS`, and `format` is `JSON_ARRAY`,
    `ARROW_STREAM` or `CSV`. The combinations are in Responses. A disposition or a
    format that is not known is ignored, and the answer has the default (recorded: "a
    disposition that is not known", "a format that is not known").
  - `parameters` is a list of `{"name", "value", "type"}` (see Parameters).
  - `query_tags` is a list of `{"key", "value"}`. The server took it (recorded: "a
    statement with query tags"). The server also took a member that does not exist
    (recorded: "a member that does not exist"), so the answer does not show that
    the tags have an effect.
- The authentication is a Bearer token in the header `Authorization`. The token is a
  personal access token (recorded: every request, where the header is redacted in the
  files). The `Accept` header was `application/json`, and the server answered JSON
  with no `Accept` too (recorded: "a request with an accept encoding of identity"). A body of
  `Content-Type: text/plain` was read as JSON (recorded: "a request with a content type
  of text"), and a body with `Content-Encoding: gzip` that was not gzip was read as plain
  JSON (recorded: "a request with a content encoding of gzip that is not gzip").
- OAuth machine to machine: the client sends `POST https://<host>/oidc/v1/token`
  with HTTP Basic authentication of the client id and the client secret of a service
  principal, and the form `grant_type=client_credentials&scope=all-apis`. The answer
  holds `access_token`, which the client then sends as a Bearer token, and
  `expires_in` of 3600 seconds (not measured, source: documentation). The token of
  the script was a personal access token, so this flow was not sent.
- A read of a statement is `GET /api/2.0/sql/statements/{statement_id}`. A read of one
  chunk is `GET /api/2.0/sql/statements/{statement_id}/result/chunks/{chunk_index}`. A
  cancel is `POST /api/2.0/sql/statements/{statement_id}/cancel`, and it answers
  `{}` (recorded: "the statement of the wait timeout of 0s, at once", "the chunk 0 of the 3000
  rows", "a cancel of the heavy statement").
- Other endpoints of the REST API that the script read as the same principal:
  - `GET /api/2.0/sql/warehouses/{id}` and `GET /api/2.0/sql/warehouses` answered
    HTTP 200 (recorded: "the warehouse", "the list of warehouses"). The answer
    holds the `state`, the size, `auto_stop_mins` of 10, `auto_resume` of `true`,
    `jdbc_url` and `odbc_params` with the host and the path
    `/sql/1.0/warehouses/<id>`.
  - `GET /api/2.0/sql/config/warehouses` answered HTTP 403 with `PERMISSION_DENIED`
    (recorded: "the configuration of the warehouses").
  - `GET /api/2.1/unity-catalog/tables` and `GET /api/2.1/unity-catalog/schemas`
    answered HTTP 200 (recorded: "the list of tables in Unity Catalog", "the list of
    schemas in Unity Catalog").
  - `GET /api/2.0/sql/history/queries` answered HTTP 200 with a page and a
    `next_page_token` (recorded: "the history of queries"). It holds the member
    `statement_type` and `client_application` of `Go-http-client`, and the text of
    each query is redacted.
- A method or a path that the API does not have answers HTTP 404. `PUT` and `GET`
  with no id, and the path `/api/2.0/sql/statements/execute`, got the code
  `ENDPOINT_NOT_FOUND` with the message "No API found for 'PUT /sql/statements'" and
  the like (recorded: "a request that is PUT", "a request that is GET with no id", "a request
  to the old path"). A path under `/api/2.0/sql/` that does not exist got HTTP 404 with an
  empty body (recorded: "a request to a path that does not exist"). `DELETE
  /api/2.0/sql/statements/{id}` answered HTTP 200 and `{}` and left the statement
  readable (recorded: "a request that is DELETE of a statement", "the first statement
  after 16 minutes"). So the API has no call that closes a statement.

## The DSN

These are the facts that a DSN must carry. The form is for step 9 and D27 to decide.

- The host of the workspace. The forms of the three clouds are:
  - AWS: `<name>.cloud.databricks.com`. The host of the workspace measured here is
    `dbc-<8 hex>-<4 hex>.cloud.databricks.com`, with the digits redacted in the files
    (recorded: "the warehouse", the member `jdbc_url`).
  - Azure: `adb-<workspace id>.<n>.azuredatabricks.net` (not measured, source:
    documentation).
  - Google Cloud: `<workspace id>.<n>.gcp.databricks.com` (not measured, source:
    documentation).
  - The port is 443 and the scheme is HTTPS (recorded: "the warehouse", the member
    `odbc_params`).
- The credential is one of:
  - A personal access token, sent as a Bearer value (recorded: every request).
  - A client id and a client secret of a service principal, which the driver exchanges
    at `/oidc/v1/token` for an access token (not measured, source: documentation).
  - `databricks-sql-go` takes the token as the user name `token` and the secret as the
    password of the URL, `token:<token>@<host>:<port>/<http path>`, and takes `clientID`
    and `clientSecret` as keys of the query for OAuth machine to machine (read of
    `doc.go` and `CONNECTION_PARAMETERS.md` of v1.16.0, 2026-10-10). D94 says that
    the secret is the password of the URL.
- The warehouse. The body takes the bare warehouse id as `warehouse_id`. It is 16
  lower case hex digits (recorded: "the warehouse"). A warehouse id that is not valid
  gets HTTP 400 and the message "nosuch is not a valid endpoint id." (recorded: "a
  warehouse id that is not valid"). An id with a valid form that names no warehouse the
  principal can use gets HTTP 403 with `PERMISSION_DENIED` and the message "You do not
  have permission to use the SQL Warehouse. Please contact your administrator."
  (recorded: "a missing warehouse"). The `http_path` of the ODBC and JDBC drivers is
  `/sql/1.0/warehouses/<id>` (recorded: "the warehouse", the member `odbc_params`). The
  older path `/sql/1.0/endpoints/<id>` is the form that `dbmeta` lists (read of
  `dbmeta/hosted/hosted.go`, 2026-10-10), and whether the Statement Execution API accepts
  either path is not measured, because the API takes the bare id.
- `catalog` and `schema`, as members of the body, both optional (see Requests).
- Anything that sets a time, such as a wait, comes from the caller for each statement
  (see Cancellation and timeouts).
- `dburl` writes `databricks://token:<token>@<host>/<path>?...`. The tests of `dburl`
  show the forms `databricks://token:key@host.cloud.databricks.com/path`,
  `databricks://token@dbc-1234.cloud.databricks.com/sql?timeout=1m` and
  `databricks://token@adb-1.2.azuredatabricks.net/sql`, with the keys `timeout` and
  `maxRows` that `databricks-sql-go` reads (read of `dburl/dburl_test.go`,
  2026-10-10).
- The warehouse can be asleep, and the first statement then waits about 15 seconds
  (see Cancellation and timeouts).

## Responses

- The answer is one JSON object. The members come in the order `statement_id`,
  `status`, `manifest`, `result` (recorded: "a statement"). The answer is not chunked
  as a stream of rows by the server. The inline rows are one array in the member
  `result.data_array`, in one body (recorded: "3000 rows of 300 characters").
- `statement_id` is a UUID text such as `01f1c46b-c6a4-1a9a-9f97-7eff37bf8459` (recorded:
  "a statement"). A statement id that is not a UUID gets HTTP 400 with "Error while
  parsing type: statement_id." (recorded: "a statement id that is not valid"), and a UUID
  that is not known gets HTTP 404 with `NOT_FOUND` (recorded: "a statement id that does
  not exist").
- `status.state` took these values: `PENDING`, `SUCCEEDED`, `FAILED` and `CANCELED`
  (recorded: "the statement of the wait timeout of 0s, at once" and others). `RUNNING`
  never arrived, because no statement stayed in the engine long enough to be
  polled. Documentation names `PENDING`, `RUNNING`, `SUCCEEDED`, `FAILED`, `CANCELED`
  and `CLOSED` (not measured, source: documentation). No recording shows `CLOSED`.
- A statement that is still `PENDING` has only `statement_id` and `status.state`
  (recorded: "a statement with a wait timeout of 0s"). A statement that failed has
  `status.error` with `error_code` and `message`, and `status.sql_state`, and no
  `manifest` and no `result` (recorded: "a syntax error"). A statement that succeeded
  and returned no rows has a `manifest` and `result` of `{}` (recorded: "the answer of a
  statement that returns no rows").
- `manifest` has `format`, `schema`, `total_chunk_count`, `chunks`, `total_row_count`
  and `truncated`. With external links it has `total_byte_count` too, and each chunk has
  `byte_count` (recorded: "a statement with the format JSON_ARRAY and the disposition
  EXTERNAL_LINKS"). With no rows, `total_chunk_count` is 0 and `chunks` is left out. `schema` has `column_count` and `columns`.
- Each column has `name`, `position`, `type_name` and `type_text`. A `DECIMAL` has
  `type_precision` and `type_scale`, and an `INTERVAL` has `type_interval_type` (recorded:
  "the decimal types", "the interval year to month types"). No column has a
  member for NULL or for the length of a string (recorded: "the rows of every type"), so
  nullability is not known.
- Names of columns: the name is the text of the alias, or the text of the expression
  when there is no alias, such as `1`, `x` and `2.5` for `SELECT 1, 'x', 2.5` (recorded:
  "columns with no name"). A name can have a space, a dot and upper case letters
  (recorded: "a column with a name that has a space and a dot"). Two columns can have one name
  (recorded: "columns that share a name"). The order is the order of the statement
  (recorded: "columns in the order of the statement"). `SELECT 1 FROM` with nothing after
  `FROM` ran and named the column `FROM` (recorded: "a syntax error at the end").
- A row is an array of text values. A NULL is JSON `null` (recorded: "a NULL of each scalar
  type", "a row of NULL in every type"). An empty string is `""` (recorded: "the string
  type"). A row keeps the order of the columns (recorded: "a row of every type").
- Dispositions and formats, as recorded for one statement:
  - `INLINE` with `JSON_ARRAY` is the default, and the rows are in `result.data_array` (recorded:
    "a statement with the format JSON_ARRAY and the disposition INLINE").
  - `INLINE` with `ARROW_STREAM` or with `CSV` gets HTTP 400 with `INVALID_PARAMETER_VALUE`
    and the message "Incompatible parameters: The format field must be JSON_ARRAY when
    the disposition field is INLINE." (recorded: "a statement with the format ARROW_STREAM
    and the disposition INLINE", "a statement with the format CSV and the disposition
    INLINE").
  - `EXTERNAL_LINKS` takes `JSON_ARRAY`, `ARROW_STREAM` and `CSV`, and `result` then
    holds `external_links` and no `data_array` (recorded: "a statement with the format
    JSON_ARRAY and the disposition EXTERNAL_LINKS", "a statement with the format ARROW_STREAM
    and the disposition EXTERNAL_LINKS", "a statement with the format CSV and the disposition
    EXTERNAL_LINKS"). The one row of `SELECT 1 AS a, 'x' AS b` had a `byte_count` of 11 in
    `JSON_ARRAY` and of 456 in `ARROW_STREAM`. The row of the `CSV` statement had four columns and a
    `byte_count` of 26.
- An external link has `chunk_index`, `row_offset`, `row_count`, `byte_count`, `external_link`
  and `expiration`. It has no `next_chunk_index` and no `http_headers` (recorded: "the
  statement of the disposition EXTERNAL_LINKS"). The `external_link` is an HTTPS URL on the host
  `us-east-2.storage.cloud.databricks.com`, which is a host of the region of the
  workspace and not the host of the workspace. Its path starts `/api/2.0/fs/files/`
  and its query holds `X-Databricks-TTL=899996`, `X-Databricks-Issued` and
  `X-Databricks-Signature` (recorded: the same request). The `expiration` is 15 minutes after the answer (recorded: "the statement of
  the disposition EXTERNAL_LINKS", where the answer came at 05:32 and `expiration` was
  05:47).
- A read of the statement, or of the chunk, gives a new link with a new `expiration`
  (recorded: "the chunk 0 of the statement of the disposition EXTERNAL_LINKS", "the statement of
  external links after 16 minutes", "the chunk 0 of the statement of external links after 16
  minutes"). A link that is older than its `expiration` is not recorded, so what
  the host of the link answers for it is not measured.
- What a driver needs to fetch a link: a `GET` of the URL, with no `Authorization` header,
  because the link carries its own signature, and the body is the chunk in the format that
  was asked (not measured, source: documentation). The recordings hold the answer that
  carries the links and never the fetch, because the recorder adds the token to
  every request. Whether the body of `JSON_ARRAY` is a JSON array of arrays of text, and
  whether a timestamp in it keeps the microseconds, are the first questions of a second
  live pass.
- Chunks: the recordings show one chunk for each statement, so the member
  `next_chunk_index` and the way to read a second chunk are not recorded. A read of
  `chunks/1` of a result with one chunk gets HTTP 400 with `INVALID_PARAMETER_VALUE` and the
  message "The chunk_index 1 is out of bounds." (recorded: "the chunk 1 of the 3000
  rows", "the chunk 1 of the rows of 300 characters", "the chunk 99 of the rows of 300
  characters", "the chunk 1 of the rows as external links"). The reason is that the
  result has one chunk. The read of chunk 0 gave the same rows as the statement
  (recorded: "the chunk 0 of the 3000 rows"). A read of a chunk of a statement that failed
  gets HTTP 404 with `NOT_FOUND` and the description "the statement state should be
  SUCCEEDED before you can start fetching the result" (recorded: "the failed
  statement, chunk 0"). Documentation says that a chunk has `next_chunk_index` and
  `next_chunk_internal_link` when another chunk follows (not measured, source:
  documentation).
- Caps and limits: `row_limit` and `byte_limit` cut the result and set `truncated`. A result
  of 3000 rows with no limit came whole in both dispositions. The rows with `byte_limit` of
  1000 were not a part of the first rows: no row arrived, and `truncated` was `true`
  (recorded: "3000 rows with the byte limit 1000"). So a `byte_limit` smaller than
  the first chunk gives an empty result, and the driver must send none.
- Compression: the server compresses with gzip when the request says
  `Accept-Encoding: gzip`, and the answer has `Content-Encoding: gzip` and `Vary:
  Accept-Encoding` (recorded: "a request with an accept encoding of gzip"). With
  `identity` the answer is plain (recorded: "a request with an accept encoding of identity").
- Redirects: no recorded answer redirected. The links are separate hosts and the
  API answered 200 or 4xx only.
- Headers of every answer include `Server: databricks`, `X-Request-Id`,
  `X-Databricks-Org-Id`, `Server-Timing` with `request_ttfb_msec`, and `Strict-Transport-Security`.
  The id of the request is the same as `request_id` in the `details` of an error.

## Types

The `type_name` is the type of the Statement Execution API. The `type_text` is the type as
Spark writes it, with its parameters and its nested types. A `type_name` and a `type_text`
differ for the integers: `TINYINT` is `BYTE`, `SMALLINT` is `SHORT` and `BIGINT` is `LONG`
(recorded: "the integer types and their limits"). Each value is text.

- `TINYINT`, `SMALLINT`, `INT` and `BIGINT` are decimal text, such as `"-128"`, `"32767"`,
  `"2147483647"` and `"-9223372036854775808"` (recorded: "the integer types and their
  limits"). A literal above `BIGINT` has the type `DECIMAL(19,0)` or `DECIMAL(20,0)`, and not
  an integer type, such as `"18446744073709551615"` (recorded: "an integer larger than
  BIGINT").
- `FLOAT` and `DOUBLE` are the text of Java, such as `"1.5"`, `"0.1"`, `"1.6777216E7"`,
  `"1.7976931348623157E308"`, `"4.9E-324"` and `"0.0"` for the negative zero (recorded: "the
  floating point types"). `NaN`, `Infinity` and `-Infinity` arrive as that text, in both
  types (recorded: "the special floating point values of DOUBLE", "the special floating point values
  of FLOAT"). The text of a `FLOAT` is the shortest text of a 32 bit value, so `0.1` stands for
  the nearest `float32`.
- `DECIMAL(p,s)` has `p` up to 38 and `s` up to 38. The text keeps the scale, such as
  `"1.50"` for `DECIMAL(10,2)` and `"0.00000"` for zero of `DECIMAL(10,5)`, and it holds
  38 digits exact, such as `"99999999999999999999999999999999999999"` and
  `"0.99999999999999999999999999999999999999"` (recorded: "the decimal types", "the longest
  decimals"). The manifest names `type_precision` and `type_scale`.
- `BOOLEAN` is the text `"true"` or `"false"` (recorded: "the boolean type"). A JSON boolean
  never arrives.
- `STRING` is UTF-8 text, and it keeps an empty string, a space, multi byte characters, a quote, a
  backslash, a newline and a tab (recorded: "the string type"). A column of `CHAR` or `VARCHAR`
  is not recorded, and the show of a table names `STRING COLLATE UTF8_BINARY` (recorded: "show
  create table"). The limit on the size of a string is not measured.
- `BINARY` is base64 text with padding, such as `"3q2+7w=="` for `DEADBEEF`, `"YWJj"` for
  `abc`, `"AA=="` for one zero byte, and `""` for an empty value (recorded: "the binary type").
- `DATE` is `"2024-01-02"`, from `"0001-01-01"` to `"9999-12-31"`, and `"1500-06-15"` is
  exact (recorded: "the date type").
- `TIMESTAMP` is an instant in UTC. The text is `"2024-01-02T03:04:05.123Z"`, with three
  digits of fraction and the letter `Z`, from `"0001-01-01T00:00:00.000Z"` to
  `"9999-12-31T23:59:59.999Z"` (recorded: "the timestamp type"). A literal with the offset
  `+05:30` arrived as the instant in UTC, `"2024-01-01T21:34:05.123Z"`, and the
  offset is not in the text. The value had six digits of fraction, and the text has three,
  cut and not rounded (`.123456` became `.123`), also for a value read from a stored
  column (recorded: "the rows of every type"). So the text loses microseconds. The session time
  zone was `Etc/UTC` (recorded: "the time zone of the session"), and a `SET TIME ZONE` did not
  reach the next request (recorded: "the time zone in the next request"), so the zone of a
  `TIMESTAMP` is always UTC for this API.
- `TIMESTAMP_NTZ` has no zone. The text is `"2024-01-02T03:04:05.123"`, with three digits and
  no `Z`, from `"0001-01-01T00:00:00.000"` to `"9999-12-31T23:59:59.999"` (recorded: "the
  timestamp_ntz type"). It loses microseconds as `TIMESTAMP` does.
- `INTERVAL` has `type_name` `INTERVAL` and the member `type_interval_type`, with the
  values `YEAR`, `MONTH`, `YEAR TO MONTH`, `DAY`, `HOUR`, `MINUTE`, `SECOND` and `DAY TO
  SECOND` (recorded: "the interval year to month types", "the interval day to second types").
  The text is an SQL literal, such as `"INTERVAL '1-2' YEAR TO MONTH"`, `"INTERVAL '-1-2'
  YEAR TO MONTH"`, `"INTERVAL '5' YEAR"`, `"INTERVAL '1 02:03:04.123456' DAY TO SECOND"`,
  `"INTERVAL '-1 02:03:04.5' DAY TO SECOND"`, `"INTERVAL '10' HOUR"` and `"INTERVAL
  '01.5' SECOND"`. A fraction of microseconds is kept. The range of a day to second
  interval is not measured (source: documentation says 106751991 days, not recorded).
- `ARRAY<T>` is a JSON array in one string, with every leaf as text: `"[\"1\",\"2\",\"3\"]"`
  for an array of `INT`, `"[]"` for an empty array, `"[null,\"1\"]"` for a NULL element
  (recorded: "the array type"). A NULL array is JSON `null` and not `"[]"` (recorded: "a NULL
  of each complex type"). `type_text` holds the element type, such as
  `ARRAY<ARRAY<INT>>`, and an empty array has `ARRAY<VOID>`.
- `MAP<K,V>` is a JSON object in one string, such as `"{\"a\":\"1\",\"b\":\"2\"}"`. The keys of a map
  of `INT` are text keys, `"{\"1\":\"x\"}"`, so the key type is only in `type_text`. An empty map
  is `"{}"` and a NULL value is `null` (recorded: "the map type"). `type_text` writes a space after
  the comma, as in `MAP<STRING, INT>`.
- `STRUCT<...>` is a JSON object in one string, with the fields in the order of the type, such as
  `"{\"a\":\"1\",\"b\":\"x\"}"`. A NULL field is `null`. `type_text` holds the names and types and the
  mark `NOT NULL`, such as `STRUCT<a: INT NOT NULL, b: STRING NOT NULL>`, and a struct with
  no names has the fields `col1`, `col2` (recorded: "the struct type"). A NULL struct is JSON
  `null` (recorded: "a NULL of each complex type").
- Nested values hold the text form of their element types: binary as base64, a timestamp with three
  digits and `Z`, a decimal with its scale, and a date, such as `"[\"3q2+7w==\"]"`,
  `"[\"2024-01-02T03:04:05.000Z\"]"`, `"[\"1.50\"]"` and `"[\"2024-01-02\"]"` (recorded: "nested
  complex types"). A struct of a map of an array stays one string with every leaf as text.
- `VARIANT` is JSON text in one string, with real JSON types: `"{\"a\":1,\"b\":[1,2,\"x\"]}"`,
  `"1.5"`, `"\"s\""` and `"null"` for a JSON null. A SQL NULL is JSON `null` (recorded: "the
  variant type", "the rows of the table with a variant column"). So a number in a `VARIANT`
  is a JSON number and a number in an `ARRAY` is text.
- `GEOMETRY` and `GEOGRAPHY` are WKT text. The column `type_text` is `GEOMETRY(0)` or
  `GEOGRAPHY(4326)`, and the text is `"POINT(1 2)"` for the geometry and
  `"SRID=4326;POINT(1 2)"` for the geography (recorded: "the geometry type", "the
  geography type"). `ST_ASTEXT` gives `"POINT(1 2)"` for both.
- A column of NULL has `type_text` `VOID` and `type_name` `NULL`, as in `SELECT NULL AS n` (recorded:
  "a statement with the format CSV and the disposition EXTERNAL_LINKS", where the
  value is not in the answer). The value in `JSON_ARRAY` is not recorded.
- A NULL is JSON `null` in every scalar and complex type (recorded: "a NULL of each scalar
  type", "a NULL of each complex type"). No value is missing from a row, because each row has one cell for each column.
- Types that models named and no recording settles: `TIME`, `CHAR(n)`, `VARCHAR(n)`, `OBJECT`
  (source: Gemini named `TIME`, `CHAR` and `VARCHAR`, and DeepSeek named `OBJECT` and said
  that `TIME` is not supported, not measured), and the type of a vector or of an
  enum. Documentation names `CHAR` and `USER_DEFINED_TYPE` and `TABLE_TYPE` as values of
  `type_name` (not measured, source: documentation).
- `databricks-sql-go` v1.16.0 scans `TINYINT`, `SMALLINT`, `INT` and `BIGINT` as `int8`, `int16`,
  `int32` and `int64`, `FLOAT` as `float32`, `DATE` and `TIMESTAMP` as `time.Time`, and `DECIMAL`,
  `BINARY`, `ARRAY`, `MAP` and `STRUCT` as `sql.RawBytes`, and an `INTERVAL` as `string`
  (read of `internal/rows/rows.go`, 2026-10-10).

The mapping is in the table below. The table was written by hand for step 8a. Step 10 will
generate it from the code. Ken decided the mapping in D193 items 6 and 7.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| TINYINT | integer | `int64` | `int64` | `TINYINT` | yes |
| SMALLINT | integer | `int64` | `int64` | `SMALLINT` | yes |
| INT | integer | `int64` | `int64` | `INT` | yes |
| BIGINT | integer | `int64` | `int64` | `BIGINT` | yes |
| FLOAT | float | `float64` | `float64` | `FLOAT` | yes |
| DOUBLE | float | `float64` | `float64` | `DOUBLE` | yes |
| DECIMAL | decimal | `*apd.Decimal` | `*apd.Decimal` | `DECIMAL` | yes |
| BOOLEAN | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| STRING | string | `string` | `string` | `STRING` | yes |
| BINARY | binary | `[]byte` | `[]uint8` | `BINARY` | yes |
| DATE | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| TIMESTAMP | timestamp | `time.Time` | `time.Time` | `TIMESTAMP` | yes |
| TIMESTAMP_NTZ | local timestamp | `dbimp.LocalDateTime` | `dbimp.LocalDateTime` | `TIMESTAMP_NTZ` | yes |
| INTERVAL YEAR TO MONTH | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| INTERVAL DAY TO SECOND | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| ARRAY | array | `[]any` | `[]interface {}` | `ARRAY` | yes |
| MAP | map | `map[string]any` | `map[string]interface {}` | `MAP` | yes |
| STRUCT | map | `map[string]any` | `map[string]interface {}` | `STRUCT` | yes |
| VARIANT | json | `the decoded JSON value` | `interface {}` | `VARIANT` | yes |
| GEOMETRY | geometry | `string` | `string` | `GEOMETRY` | yes |
| GEOGRAPHY | geometry | `string` | `string` | `GEOGRAPHY` | yes |
| VOID | null | `nil` | `interface {}` | `VOID` | yes |
<!-- /dbimp:types -->

Notes on the mapping, which step 9 must settle:

- Each value arrives as text, so every type needs a reader of text. An error in the text is an
  error of the row (D135).
- `FLOAT` has the text of a 32 bit value. The kind float gives `float64`, so `0.1` is read as the
  `float64` nearest to the text, and not as the `float32` widened. Both are choices for step 9.
  `NaN`, `Infinity` and `-Infinity` need a reader that takes them, and `strconv.ParseFloat`
  does.
- Decided, D193: the driver accepts three digits. `TIMESTAMP` and `TIMESTAMP_NTZ` have three digits of fraction in `JSON_ARRAY`, so a value with
  microseconds loses them in the server before the driver sees them. The Go types hold
  nanoseconds. A column in a stored table keeps the microseconds on the server (recorded:
  "the rows of every type" shows the stored value was cut in the answer, and the statement
  that stored it had six digits).
- Decided, D193: both interval types are a `dbimp.Interval`. A year to month interval has months only, and a day to second
  interval has days and nanoseconds. The types `INTERVAL YEAR`, `INTERVAL MONTH`, `INTERVAL DAY`, `INTERVAL HOUR`, `INTERVAL MINUTE` and
  `INTERVAL SECOND` have their own `type_interval_type` and the same kind.
- A `DECIMAL` has up to 38 digits, so `*apd.Decimal` holds every value (D33).
- `MAP` has keys of any type, and the text has text keys, so the key type of a map of `INT` is lost, and the keys of a map
  of `BINARY` stay base64 text.
- Decided, D193: a `STRUCT` is a `map[string]any`, a `MAP` is a `map[string]any` that loses its key type, and an `ARRAY` is a `[]any`.
  The driver parses `type_text` to decode them. A struct with two fields of one name is not recorded.
- Every leaf of an `ARRAY`, `MAP` or `STRUCT` is text, so the driver must parse `type_text`
  (`ARRAY<STRUCT<a: MAP<STRING, ARRAY<INT>> NOT NULL>>`) and decode each leaf by its type. The
  grammar has a space after a comma, `: ` between a name and a type, and `NOT NULL`.
- `VARIANT` is decoded JSON with real numbers, so a number goes through `dbimp.Number` (D19). A
  `VARIANT` that holds a JSON null and a SQL NULL both become `nil`, and the kind json cannot tell
  them apart. Decided, D193: both are `nil` (D18).
- `GEOMETRY` and `GEOGRAPHY` are text with a different form for the SRID, and the kind geometry
  leaves the Go type to the decision of the driver. The table gives `string`, as Databend does.
- `VOID` is the type of a bare `NULL`, and the value of such a column is not recorded in `JSON_ARRAY`.
- A column has no nullability, so the table says `yes` for every type.

## Parameters

The server binds parameters. The request carries `parameters`, a list of objects with `name`,
`value` and `type`.

- A named marker is `:name` in the text, and the parameter has the same `name`. Two
  parameters worked, and one name can be used twice in the text (recorded: "two parameters", "a
  parameter used twice"). A marker with no parameter makes the statement fail with the error
  `UNBOUND_SQL_PARAMETER` and the state `42P02` (recorded: "a marker with no parameter"). A
  parameter with no marker is ignored (recorded: "a parameter with no marker"). The same
  name twice makes the statement fail with `INVALID_PARAMETER_MARKER_VALUE.DUPLICATE_NAME`
  (recorded: "a parameter with the same name twice").
- A positional marker is `?`, and its parameter has no `name` (recorded: "a positional marker"). A
  mix of named and positional markers is not recorded.
- `value` is text. `type` is the SQL type as text. With no `type`, the type is `STRING` (recorded: "a
  parameter with no type", where `typeof` answered `string`).
- These types were accepted, and the server kept the type (recorded: "a parameter of the type STRING", "a parameter of the type INT", "a parameter of the type BIGINT", "a
  parameter of the type FLOAT", "a parameter of the type DOUBLE", "a parameter of the type DECIMAL", "a parameter of the
  type DECIMAL with 38 digits", "a parameter of the type BOOLEAN", "a parameter of the type DATE", "a parameter of the
  type TIMESTAMP", "a parameter of the type TIMESTAMP_NTZ", "a parameter of the type INTERVAL DAY TO SECOND", "a
  parameter of the type INTERVAL YEAR TO MONTH"). The type `DECIMAL(10,2)` with the value `1.50` gave `decimal(3,2)`, so the
  server takes the precision from the value (recorded: "a parameter of the type DECIMAL"). A
  `DECIMAL(38,18)` kept its 38 digits (recorded: "a parameter of the type DECIMAL with 38
  digits"). A `TIMESTAMP` parameter with microseconds came back with milliseconds (recorded: "a
  parameter of the type TIMESTAMP"), as a column does.
- These types were refused with the state `FAILED` and `INVALID_PARAMETER_MARKER_VALUE.INVALID_DATA_TYPE`:
  `BINARY`, `ARRAY<INT>`, `MAP<STRING,INT>`, `STRUCT<a:INT>` and a type that does not exist (recorded: "a parameter of the type BINARY", "a parameter of the type ARRAY", "a parameter of the type MAP", "a
  parameter of the type STRUCT", "a parameter of a type that does not exist"). `databricks-sql-go` sends none of them either (read of `parameters.go`, 2026-10-10).
- A `VARIANT` parameter with the value `{"a":1}` came back as `"{"` and the type `variant`, so
  the server read the text as a string and the answer is not the value (recorded: "a parameter of
  the type VARIANT"). It is a fault of the server.
- A NULL is a parameter with `value` of `null` or with no `value`, and a `type` (recorded: "a parameter
  with the value NULL as a JSON null", "a parameter with no value"). A parameter with no `type` and no
  `value` is not recorded.
- A value that does not fit its type fails the statement with `INVALID_VALUE_FOR_DATA_TYPE` and the
  state `22023` (recorded: "a parameter that does not fit its type"). A quote, a semicolon and a comment
  in a string value stayed a value (recorded: "a parameter with a quote and a semicolon in the value").
- A marker can stand for a value in a `WHERE` and in an `INSERT` (recorded: "a marker in a where
  clause", "a marker in an insert"). A marker as an identifier works through `IDENTIFIER(:t)`, with a
  `STRING` parameter that holds the name (recorded: "a marker as an identifier"). - An empty list of parameters is accepted (recorded: "a parameter list that is empty").
- The server binds both kinds of marker, so the parser for placeholders of D34 is not needed.

## Transactions

- There is no session. A fresh session answers each request, and nothing set in it reaches the next
  request (recorded: "the division by zero with the ansi mode off, in the next request", "the time zone
  in the next request", "the schema in the next request", "the catalog in the next request", "the temporary view in the
  next request", "the variable in the next request"). The statements `SET ansi_mode`,
  `SET TIME ZONE`, `USE SCHEMA`, `USE CATALOG`, `CREATE TEMPORARY VIEW` and `DECLARE VARIABLE`
  each succeeded alone (recorded: "a set of the ansi mode", "a set of the time zone", "a use schema", "a use
  catalog", "a temporary view in one request", "a declared variable in one request"), and `SET` answers
  one row with `key` and `value`. The body that the server accepted has no member for a session (recorded: "a statement with every optional member").
  The server ignores a member that does not exist (recorded: "a member that does not exist"), so no recording shows
  that a member for a session is absent. The history of queries shows a `session_id` for each statement, and
  this work did not try to send it (recorded: "the history of queries").
- A `SET` of a configuration that the warehouse does not allow fails with `CONFIG_NOT_AVAILABLE` (recorded: "a
  set of a configuration").
- A transaction does not last across requests. `BEGIN TRANSACTION` succeeded in one request and
  `COMMIT` in the next failed with `NO_ACTIVE_TRANSACTION` (recorded: "a begin transaction", "a commit").
  `ROLLBACK` succeeded with nothing to roll back (recorded: "a rollback"). `START TRANSACTION` is not valid
  (recorded: "a start transaction"). A bare `BEGIN` is a syntax error, because `BEGIN` opens a script block
  (recorded: "a begin"). `SET AUTOCOMMIT = false` succeeded with no effect that the recordings show
  (recorded: "a set of autocommit").
- A script can hold `BEGIN ATOMIC ... END`. It failed for a table that does not have the table
  feature `catalogManaged`, with `TRANSACTION_NOT_SUPPORTED.WRITE_NON_CATALOG_MANAGED_TABLE` (recorded: "a
  begin atomic"). A script that holds `BEGIN TRANSACTION`, `COMMIT` or `ROLLBACK` fails with
  `TRANSACTION_NOT_SUPPORTED.SQL_SCRIPT_TRANSACTION_COMMAND` and the message that names `BEGIN ATOMIC` (recorded:
  "a script that begins a transaction", "a script that rolls back"). Whether `BEGIN ATOMIC` works on a table with
  the feature is not measured.
- So a driver has no transaction. `BeginTx` can return the error that D20 names. The one atomic
  unit is a script of `BEGIN ATOMIC ... END`, which is one statement, and it needs a table of
  Unity Catalog with the feature (not measured on such a table).
- `databricks-sql-go` v1.16.0 has `Begin` and `BeginTx` that return `ErrNotImplemented`. Its repository holds a
  design document for transactions with the status "in review" (read of `connection.go` and
  `DESIGN_MULTI_STATEMENT_TRANSACTIONS.md`, 2026-10-10).
- A DML statement took effect when its request ended (recorded: "an insert and its answer", "a select of the
  rows").

## Errors

- A statement that fails in the engine is HTTP 200 with the state `FAILED`. The error is in
  `status.error`, and the state of SQL is in `status.sql_state`:
  `{"status":{"state":"FAILED","error":{"error_code":"BAD_REQUEST","message":"..."},"sql_state":"42601"}}` (recorded:
  "a syntax error"). The `error_code` was `BAD_REQUEST` for all 53 failed statements, whatever the cause. A
  driver must read `sql_state` and the text of the message.
- A statement that is `PENDING` can fail later, so the error can arrive in a later read (recorded: "an error with a wait
  timeout of 0s", "the failed statement of the wait timeout of 0s, after 5s"). A failed statement read again
  gives the same error (recorded: "a failed statement that is read again", "the failed statement, read again").
- The message has the name of the error in brackets and the state, such as `[PARSE_SYNTAX_ERROR] Syntax
  error at or near 'SELEC'. SQLSTATE: 42601 (line 1, pos 0)`, with a copy of the statement and the position
  after it. These are the errors recorded, with their state:
  - `PARSE_SYNTAX_ERROR`, `42601`, and `PARSE_EMPTY_STATEMENT`, `42617` (recorded: "a syntax error", "an empty
    statement").
  - `TABLE_OR_VIEW_NOT_FOUND`, `42P01` (recorded: "a table that does not exist").
  - `UNRESOLVED_COLUMN.WITH_SUGGESTION`, `42703` (recorded: "a column that does not exist").
  - `UNRESOLVED_ROUTINE`, `42883` (recorded: "a function that does not exist").
  - `DIVIDE_BY_ZERO`, `22012`, `ARITHMETIC_OVERFLOW`, `22003`, and `CAST_INVALID_INPUT`, `22018` (recorded: "a
    division by zero", "an overflow of an integer", "a cast of text to an integer"). The messages name `ansi_mode`
    and `try_cast`.
  - `TABLE_OR_VIEW_ALREADY_EXISTS`, `42P07` (recorded: "a duplicate table").
  - `USER_RAISED_EXCEPTION`, `P0001` (recorded: "a raised error").
  - `NOT_SUPPORTED_WITH_DB_SQL`, `0A000`, for `java_method` (recorded: "the statement that sleeps, at once").
  - `INSUFFICIENT_PERMISSIONS`, `42501`, and a message that starts `PERMISSION_DENIED:` with the same state (recorded: "a
    create table in the schema of another user", "a create schema", "show tables in the schema of another user").
- An error after some rows: the statement fails as a whole, and no rows arrive. A division by zero in
  the sixth row of 10 and one in the last row of 3000 both gave `FAILED` and no `manifest` (recorded: "a division by zero
  after some rows", "an error in the middle of a large result"). So the driver has no error after some rows
  in the inline form.
- HTTP statuses before a statement runs:
  - 400 with `error_code` as text, `message`, and `details` that name the field or the `request_id`:
    `INVALID_PARAMETER_VALUE` (a wait timeout, a chunk out of bounds, a row limit, a byte limit, a warehouse id,
    a statement id, a disposition with a format), `BAD_REQUEST` (a field that is required) and
    `MALFORMED_REQUEST` for a body that is not JSON, with the message "Invalid JSON given in the body of the request
    - failed to parse given JSON" (recorded: "a body that is not JSON").
  - 403 with `PERMISSION_DENIED` for a warehouse that the principal cannot use and for the configuration of the
    warehouses (recorded: "a missing warehouse", "the configuration of the warehouses").
  - 403 for a wrong token, with the body `{"error_code":403,"message":"Invalid access token. [ReqId:
    ...]"}`, and the header `X-Databricks-Reason-Phrase: Invalid access token.` (recorded: "a statement with a wrong
    token", "a read of a statement with a wrong token", "a cancel with a wrong token", "the warehouse with a wrong
    token"). The status is not 401, and `error_code` is a JSON number here and a text everywhere else. A decoder
    must accept both.
  - 404 with `NOT_FOUND` for a statement or a result that is not known, with `details` that give the resource
    (recorded: "a statement id that does not exist", "a chunk of a statement that does not exist"), and 404 with
    `ENDPOINT_NOT_FOUND` for a path or a method that the API lacks.
- A limit on the rate of requests, and HTTP 429 or 503, did not appear. The script sent its requests one at a time. The behavior at a limit is not measured.
- Which errors mean that the request did not reach the server: a refusal of the connection or of the TLS
  handshake before any byte of the request was written (not measured: no recording holds a connection error). An
  HTTP status means that the server read the request. A statement that is `PENDING` was accepted, so a driver must
  not send it again.
- The error of a read of a statement that has `CANCELED` is not an error object. The state is the only sign
  (recorded: "the canceled statement, after 3s").

## Cancellation and timeouts

- A statement can wait on the server. With `wait_timeout` of `"0s"` the answer is at once with the state
  `PENDING`, and a later `GET` gives the result (recorded: "a statement with a wait timeout of 0s", "the statement of
  the wait timeout of 0s, at once", "the statement of the wait timeout of 0s, after 5s"). With `"5s"` to `"50s"` the call
  waits until the statement ends or the time passes (recorded: "a statement with a wait timeout of 5s", "a statement
  with a wait timeout of 50s"). The call that was recorded for the first statement waited about 15 seconds, within
  the 50 (recorded: "setup: warm the warehouse").
- What happens when the wait passes and the statement is still running: `on_wait_timeout` is `CONTINUE` by
  default, or `CANCEL`. Both statements that the script sent for this finished within 5 seconds, so the answer
  was `SUCCEEDED` for both and the cases are not measured (recorded: "a heavy statement with a wait timeout of 5s
  and CANCEL", "a heavy statement with a wait timeout of 5s and CONTINUE"). Documentation says that `CANCEL`
  cancels the statement when the wait ends and `CONTINUE` lets it run (not measured, source:
  documentation).
- A poll is `GET /api/2.0/sql/statements/{id}`. The call answers at once and does not wait (recorded: "the statement
  of the wait timeout of 0s, at once"). The documentation has no query parameter for a wait on a read (not
  measured, source: documentation), so a driver polls with a delay.
- A cancel is `POST /api/2.0/sql/statements/{id}/cancel`. It answers HTTP 200 and `{}` for a statement that runs,
  for a statement that finished, and for an id that does not exist (recorded: "a cancel of the heavy statement",
  "a cancel of a statement that is finished", "a cancel of a statement that does not exist"). The statement
  was `CANCELED` after 3 seconds (recorded: "the canceled statement, after 3s"). A cancel of a finished statement
  left it `SUCCEEDED` (recorded: "the first statement after 16 minutes", a read of the statement that "a cancel of a
  statement that is finished" cancelled). A cancel of a statement that
  was `PENDING` at once worked, and a cancel of one that is `RUNNING` is not recorded, because the statements
  that the script made ended too soon.
- What the server does when the client leaves: a statement sent with `wait_timeout` of `"0s"` runs to its end with
  no client attached (recorded: "the statement of the wait timeout of 0s, after 5s"). What a statement with a wait of
  `"50s"` does when the client closes the connection is not recorded. So a driver cancels on the server by
  its own cancel, as D36 says.
- Waking the warehouse: the first statement after the warehouse stopped took 14992 ms by the header
  `Server-Timing` (recorded: "setup: warm the warehouse"), and the first statement after the pause of 16 minutes
  took 15131 ms (measured: the header `Server-Timing` of "teardown: drop view dbimp_t_view", 2026-10-10). The warehouse
  has `auto_stop_mins` of 10 and `auto_resume` of `true` (recorded: "the warehouse"). A client that waits at most
  50 seconds for a statement waits long enough for this wake.
- Statement lifetime: an inline result that was 21 minutes old was still readable (measured: "the first statement
  after 16 minutes" is a read at 05:53:14 of the statement made at 05:31:59, 2026-10-10). A read of the
  statement and of its chunk 0, 16 minutes after, gave the same rows (recorded: "a statement after 16 minutes",
  "the chunk 0 of the statement after 16 minutes"). The end of the life of a statement is not measured, and no
  `CLOSED` state was seen.
- A read of a statement with external links, 16 minutes after, gave a fresh link (see Responses).
- `databricks-sql-go` has the option `WithTimeout`, a time for a query on the server in seconds, and `maxRows`
  of 100000 for each fetch (read of `CONNECTION_PARAMETERS.md`, 2026-10-10).

## Statements

- Several statements in one request: `SELECT 1 AS a; SELECT 2 AS b` fails with `PARSE_SYNTAX_ERROR` and the words
  "extra input 'SELECT'", and two `INSERT` statements run neither (recorded: "two statements in one request", "two
  statements that change data", "the rows after the two statements"). A script in a block runs: `BEGIN DECLARE x INT
  DEFAULT 1; SET x = x + 1; SELECT x; END` gave one row with `"2"`, and a block with an `INSERT` and a `SELECT` gave
  the count (recorded: "a script with a compound statement", "a script that changes data"). The answer of a script is
  the result of its last statement. A script with an error in the middle fails as a whole and returns no
  earlier rows (recorded: "a script with an error in the middle").
- A trailing semicolon, a line comment first or last, and a block comment are accepted (recorded: "a statement with a
  trailing semicolon", "a statement with a trailing semicolon and a comment", "a statement with a line comment first", "a
  statement with a line comment last", "a statement with a block comment"). A statement that is only a comment, only a
  semicolon, or only white space fails with a syntax error or `PARSE_EMPTY_STATEMENT` (recorded: "a statement that is
  only a comment", "a statement that is only a semicolon", "a statement that is only white space").
- The count of rows of a DML statement is a row of the result, and the column names depend on the statement:
  - `INSERT` gives `num_affected_rows` and `num_inserted_rows`, both `BIGINT`, as text (recorded: "an insert and its
    answer", "an insert from a select").
  - `UPDATE` and `DELETE` give `num_affected_rows` only (recorded: "an update and its answer", "a delete and its answer").
  - `MERGE` gives `num_affected_rows`, `num_updated_rows`, `num_deleted_rows` and `num_inserted_rows` (recorded: "a merge
    and its answer"), where the row was `["2","1","0","1"]`.
  - `TRUNCATE`, `CREATE TABLE`, `CREATE VIEW`, `ALTER TABLE` and `DROP` give no column and no row (recorded: "a
    truncate", "a create table of Delta", "a create view", "an alter table that adds a column").
  - `CREATE TABLE AS SELECT` names the two columns of `INSERT`, and gives no row, so the count is not known
    (recorded: "a create table as select").
  - The answer does not say that the statement was DML. A `SELECT 1 AS num_affected_rows` has the same shape, so a
    driver that must tell the two apart reads the first word of the statement, or the history of queries, which names
    a `statement_type` (recorded: "the history of queries").
  - The count of an update that matched no row is not recorded.
- There is no id of the last row inserted. A column `GENERATED ALWAYS AS IDENTITY` gives values 1 and 2 (recorded: "the rows
  of the table with an identity column").
- The statements that the script ran, each one alone:
  - `CREATE TABLE ... USING DELTA`, `CREATE TABLE AS SELECT`, `CREATE VIEW`, `DROP VIEW`, `ALTER TABLE ADD COLUMN`, and
    `ALTER TABLE RENAME TO` (recorded: "a create table of Delta", "a create table as select", "a create view", "a drop of
    the view", "an alter table that adds a column", "an alter table that renames the table").
  - `ALTER TABLE RENAME COLUMN` and `DROP COLUMN` need the table property `delta.columnMapping.mode` set to `name`
    first, and then they work (recorded: "an alter table that renames a column", "an alter table that renames a column
    after the mapping", "an alter table that drops a column"). The request that is named "an alter table that
    renames a column" holds the `SET TBLPROPERTIES` statement, and it succeeded. The script then sent the rename.
  - A primary key and a foreign key are accepted, and neither one is enforced: two rows with one key and a
    key that has no parent both went in (recorded: "a create table with a primary key", "a create table with a foreign
    key", "an insert of a duplicate primary key", "an insert of a foreign key that has no row"). The constraints show
    in `information_schema.table_constraints` and `key_column_usage` (recorded: "the constraints of the tables", "the
    key columns of the foreign key").
  - A generated column, an identity column, a default (with the table property `delta.feature.allowColumnDefaults`)
    and a SQL function work (recorded: "a create table with a generated column", "a create table with an identity
    column", "a create table with a default", "a call of the function"). A table function with a parameter
    failed for a reason of the body (recorded: "a create function that returns a table"), so table functions are not
    settled.
  - `EXPLAIN` gives rows of text, one for each line (recorded: "an explain").
  - `DESCRIBE HISTORY` failed because the table had been renamed first, so it is not settled (recorded: "the history of
    the table").
- INFORMATION_SCHEMA and SHOW:
  - `workspace.information_schema` has `schemata`, `tables`, `columns`, `table_constraints` and `key_column_usage`, and
    each one answered (recorded: "the schemata", "the tables in information_schema", "the columns in
    information_schema", "the constraints in information_schema", "the constraints of the tables"). `columns` has
    `ordinal_position` from 0, `is_nullable` as `YES`, `data_type` and `full_data_type` as text, and `numeric_precision`
    and `numeric_scale` as NULL for a type with none (recorded: "the columns in information_schema").
  - `SHOW CATALOGS` gives the column `catalog`, `SHOW SCHEMAS IN workspace` gives `databaseName`, `SHOW TABLES IN
    workspace.dbimp` gives `database`, `tableName` and `isTemporary`, `DESCRIBE TABLE` gives `col_name`, `data_type`
    and `comment`, and `SHOW CREATE TABLE` gives one cell `createtab_stmt` (recorded: "show catalogs", "show schemas",
    "show tables", "describe table", "show create table"). `SHOW CATALOGS` named `samples`, `system` and `workspace`.
  - The version: `SELECT version()` gives one row and one column with the text `4.2.0 <hash>` (recorded: "the
    version"). D181 item 1 says a product with a query for its release gets no support of the driver.
- The statement `SELECT current_catalog(), current_schema(), current_database()` gave `workspace`, `dbimp`, `dbimp` for
  the request that set both members (recorded: "the catalog and the schema").
- Unity Catalog has three levels: catalog, schema, table. `current_database()` is the schema (recorded: "the catalog
  and the schema").

## Principals

- The script has one login: a personal access token of one service principal. The principal owns the schema `dbimp` in
  the catalog `workspace`, and holds `USE CATALOG` on the catalog only (recorded: "the schemata", where `schema_owner`
  of `dbimp` is the principal). `dbrun` makes no ordinary user for a hosted service, and the manifest says so in
  `noOrdinaryUser`. Every recording is of that login.
- What the principal can do: read and write its own schema, create and drop tables, views and functions in it, use the
  warehouse, and call the warehouse API and the Unity Catalog API for reading (recorded: "a create table of Delta", "an insert and its answer", "a create view", "a create function", "the
  warehouse", "the list of tables in Unity Catalog").
- What it cannot do:
  - Browse a schema of another owner: `SHOW TABLES IN workspace.dbmeta` fails with `PERMISSION_DENIED: User does not
    have BROWSE on Catalog 'workspace'.` (recorded: "show tables in the schema of another user").
  - A read of a table of that schema fails with `TABLE_OR_VIEW_NOT_FOUND`, as if it did not exist, so the server does not
    tell that the schema exists (recorded: "a select from the schema of another user").
  - Create a table there: `INSUFFICIENT_PERMISSIONS`, `USE SCHEMA` (recorded: "a create table in the schema of another
    user"). Create a schema: `PERMISSION_DENIED`, `CREATE SCHEMA` (recorded: "a create schema").
  - Read the configuration of the warehouses: HTTP 403 (recorded: "the configuration of the warehouses").
- The version is open to the principal (recorded: "the version").
- A principal that has no right to use the warehouse gets HTTP 403 (recorded: "a missing warehouse").
- A personal access token is the only credential that was sent. The token of a service principal for OAuth has
  `all-apis` as its scope (not measured, source: documentation).

## Flavors

None. The API is one service. Databricks on AWS, on Azure and on Google Cloud use the same API with a different
host (see The DSN), and only AWS was measured. A host on Azure or Google Cloud is not measured.

## Interfaces

Not written yet. Step 10 generates the table.

## Faults

The faults of `github.com/databricks/databricks-sql-go` v1.16.0, which this driver must not repeat (read of the
source in the module cache, 2026-10-10):

- `Begin` and `BeginTx` return `ErrNotImplemented`, with `context.TODO()` in the error. This driver must return
  the error that D20 names, with the context of the caller.
- `LastInsertId` always returns 0, and `RowsAffected` returns the field that the server gave. A caller cannot tell a
  count of 0 from an unknown count.
- A column of `TINYINT`, `SMALLINT` and `INT` scans as `int8`, `int16` and `int32`, and a `FLOAT` as `float32`. D135 gives
  `int64` and `float64`. A `DECIMAL`, a `BINARY`, an `ARRAY`, a `MAP` and a `STRUCT` scan as `sql.RawBytes`, and an
  `INTERVAL` as `string`. The decimal is a text and not a number.
- `ColumnTypeNullable` returns `false, false` for every column, and the manifest of the REST API has no nullability
  either.
- The parameter of a Go `int` goes as `INT`, and a `uint64` goes as `BIGINT`, so a large value wraps or fails. A
  `time.Time` goes as `TIMESTAMP` in RFC 3339 text. A `[]byte` goes as a `STRING` of its bytes with the verb `%s`,
  because the server refuses a `BINARY` parameter. A value of another type goes as text with `%s`.
- The default backend is Thrift over HTTP with Arrow batches, and a second backend needs Rust libraries that the
  build tag `databricks_kernel` pulls in (read of `CONNECTION_PARAMETERS.md`). The module needs Arrow, which is the
  cost that `usql` lists.
- The driver opens a session on the server for each connection, so a `SET` and a `USE` last for the connection. Its
  `ResetSession` returns `nil` and does nothing, so state that one caller set stays for the next caller that gets the
  connection from the pool. This driver has no session (see Transactions). Its `Ping` runs `select 1`.
- `maxRows` of 100000 is a client setting for the size of each fetch, and a driver here has no such setting
  (see Responses).
- `usql` has no `Version` statement and no hook for Databricks, except an error hook.
- The warehouse has faults of its own that the sections above list: the timestamp text with three digits, a
  `VARIANT` parameter that comes back as `"{"`, a primary key that is not enforced, an `error_code` of 403 as a number
  for a wrong token, and the `error_code` of `BAD_REQUEST` for every failed statement.

## Second opinions

- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) was asked the four questions of step 5a. It named the statements of
  CRUD, the schema features, the features and the types that `features.json` holds. It said that a primary key, a
  foreign key and a unique constraint are informational and not enforced, which the server showed for the first two
  (recorded: "an insert of a duplicate primary key", "an insert of a foreign key that has no row"). It named a `TIME`
  type and said that it was unsure whether a column can have it. It said that parameter markers are named `:param`
  and positional `?`, which the server confirmed. It said that `CHAR(n)` has `n` up to 255 and that `VARCHAR(n)` goes to a
  billion characters, which is not measured.
- DeepSeek (`deepseek-v4-flash`, 2026-10-10) was asked the same four questions. It gave the same operations, and it
  listed `QUALIFY`, `table_changes`, `read_files`, `OPTIMIZE`, `VACUUM` and the AI functions. It said that `TIME` is not
  supported and that `OBJECT` is unknown, and it said that a unique constraint and a default value were uncertain. The server
  accepted a default value on a table with a property (recorded: "a create table with a default"). It also said that a
  parameter marker `?` was uncertain, and the server accepted it (recorded: "a positional marker").
- `deepseek-v4-pro`, `kimi-k3`, `kimi-k2.7-code`, `qwen-max` and `qwen` timed out on every try on 2026-10-10, and
  `deepseek-v4-pro` first used its whole budget of tokens for its thinking, so they gave no answer. `deepseek-v4-flash` needed a
  budget of 20000 tokens.
- `github.com/databricks/databricks-sql-go` v1.16.0 was read on 2026-10-10 in the module cache. No driver in another
  language was read (the Python connector, the JDBC driver and the Go SDK were not), so the survey has no client in another
  language. The reference of the API stands in for it, as it did for Snowflake and BigQuery.
- Gemini (2026-10-10) was asked what step 6 did not find. It gave these leads, and the server said:
  - Cancel with `POST .../cancel`, poll with `GET`, fetch `chunks/1`, set `on_wait_timeout` to `CANCEL`, and set
    `row_limit` and `byte_limit`. All were recorded, and the chunk 1 does not exist for a result of one chunk (recorded:
    "a cancel of the heavy statement", "the chunk 1 of the 3000 rows", "3000 rows with the row limit 10", "3000 rows with the byte
    limit 1000"). The cases of `on_wait_timeout` ended before the wait (see Cancellation and timeouts).
  - Force a result of several chunks with `SELECT repeat('A', 10000) FROM range(2000)`, which is 20 MB, and Gemini said
    that the limit of INLINE is 16 MB. The recordings show the byte limit of 25 MiB for the request (recorded: "3 rows
    with the byte limit 0"). The lead is not sent. A result above 25 MiB needs a statement of about 30 MB.
  - Send two statements in one request, and see whether it fails: it fails (recorded: "two statements in one request").
  - The NULL, NaN, Infinity, decimal, interval and timestamp text: all were recorded (recorded: "a NULL of each scalar type",
    "the special floating point values of DOUBLE", "the longest decimals", "the interval year to month types", "the timestamp
    type").
  - A statement with zero rows: recorded (recorded: "columns of a result with no rows").
  - A limit on the size of the request, which answers HTTP 413 for a statement of 10 MB. Not sent, and not measured.
- DeepSeek (2026-10-10) was asked the same question. It gave these leads that the script sent: the combinations of
  `format` and `disposition`, the parameter types, the refusals of `BINARY`, `ARRAY`, `MAP` and `STRUCT` (it was right), the
  warehouses endpoint and the history endpoint, `catalog` and `schema`, and the text of `INTERVAL`, `VARIANT`,
  `GEOMETRY`, `GEOGRAPHY` and `BINARY` (it said "likely" and was right). It gave leads that nothing has sent:
  - `SELECT * FROM t VERSION AS OF 123` for time travel, `CREATE TABLE t SHALLOW CLONE src` for a clone,
    `SELECT v:field::string FROM t` for a path of `VARIANT`, and `SELECT ... |> WHERE ...` for pipe syntax.
  - `next_chunk_index` and `next_chunk_internal_link` of a result of several chunks.
  - A request member `idempotency_key`, which the documentation does not have as far as the agent knows. The server
    ignores a member that does not exist (recorded: "a member that does not exist"), so a test cannot tell.
  - HTTP 429.
  - `TIME`, `UUID`, `JSON` and `OBJECT` as native types.
  Each of these stays "not measured", with the model that gave it.
- The leads of this work, which the second live pass must send (see Open questions):
  - A result of more than one chunk, with `next_chunk_index` and `next_chunk_internal_link`, for each disposition.
  - The body of a presigned link of `JSON_ARRAY`, fetched with no `Authorization` header, and the text of a timestamp in
    it.
  - A statement that stays `RUNNING` for more than 10 seconds (for example a cross join of 200000 rows with
    itself, or `SELECT count(*) FROM range(1000000000)` with a hash), to read `RUNNING`, a wait that passes
    with `CONTINUE` and with `CANCEL`, and a cancel of a running statement.
  - A catalog or a schema that does not exist in the body, with `SELECT current_schema()`.
  - A column of `CHAR(n)`, `VARCHAR(n)` and a type `TIME`, in a table, to see the `type_name` and the text.
  - The value of `SELECT NULL AS n` in `JSON_ARRAY`.
  - A `BEGIN ATOMIC` script on a table with the table feature `catalogManaged`.
  - `OAuth` machine to machine at `/oidc/v1/token` with `scope=all-apis`.
  - The Azure and Google Cloud hosts.
  - `INSERT OVERWRITE`, `COPY INTO`, `REPLACE WHERE`, `CHECK` and a unique constraint, `OPTIMIZE`, `VACUUM`, `RESTORE`, time
    travel, `SHALLOW CLONE`, a path of `VARIANT`, `QUALIFY`, pipe syntax, a materialized view, a streaming table,
    partitioning and clustering, and an index: none was sent.
  - `DESCRIBE HISTORY` on a table that is not renamed, and a table function with a body that the server accepts.
  - `CREATE SCHEMA` as a principal that can create one, to settle the feature, because the refusal recorded is a
    privilege refusal.
  - The effect of `query_tags` (the member of the history of queries named `query_tags` was empty).
  - A `VARIANT` parameter from a `STRING` with `parse_json(:p)`, and a `BINARY` through `unhex(:p)`.
  - A DML statement that changes no row, and `CREATE TABLE AS SELECT` with the count.
  - A limit on the rate, HTTP 429.
  - The end of the life of a statement, and a `CLOSED` state.
  - A mix of named and positional markers.
- Gemini (`gemini-3.1-pro-preview`, 2026-10-10) and DeepSeek (`deepseek-v4-flash`, 2026-10-10) each reviewed the type
  mapping of step 8a in their own conversation, from the kinds of `TYPES.md`, D135 and the text that the server sent.
  The first Gemini call and the second one closed the connection once each, and the answer was cut by its token budget
  after the item for `MAP`, so its review has two parts:
  - Both said that `INTERVAL DAY TO SECOND` is a mistake as `time.Duration`, and that it must be a `dbimp.Interval`
    with days and nanoseconds, because a day can differ in length and because `time.Duration` overflows above about
    292 years. They were told only the shape of the text. When Gemini was told that Spark stores a day to second
    interval as an exact count of microseconds, so a day is 24 hours, it said that this changes its view, and that the
    type then measures an absolute duration. That fact is not measured here (source: Gemini and the Spark
    documentation as the agent knows it). Decided, D193: the mapping is a `dbimp.Interval`.
  - DeepSeek said that a `VARIANT` that holds a JSON null and a SQL NULL must not both become `nil`, and that a number
    in a `VARIANT` must not go through a `float64`. The mapping already decodes the number with `dbimp.Number` (D19), as
    the kind json says, and the JSON null is the question of D18, which is open. It said that the parser of `type_text`
    must strip `NOT NULL`, that the key type of a `MAP` is lost and cannot be recovered with a `map[string]any`, that
    the SRID of a `GEOGRAPHY` must be kept or stripped by a decision, that `strconv.ParseFloat` takes `Infinity`, that a
    `BINARY` is base64 to decode, and that the lost microseconds of a `TIMESTAMP` cannot be recovered by any mapping. It
    found no other kind wrong.
  - Gemini, in its second part, gave a definition of each remaining type and found no fault in `ARRAY`, `STRUCT`,
    `VARIANT`, `GEOMETRY`, `GEOGRAPHY`, `TIMESTAMP`, `FLOAT`, `VOID` and `BINARY`. In the first part it said that the
    leaves of a `MAP` must be decoded by their type from `type_text`, which the notes say.
  - The two reviewers are of two vendors, Google and DeepSeek. Neither one saw the recordings, so neither one tested a
    claim about the server.

## Open questions

Ken decided the step 9 items on 2026-10-10, in D193. These are the answers, and what stays open.

- Decided, D193: `INLINE` only, with an error on `truncated` or on a statement that fails for the size of the result. A
  later release can add `EXTERNAL_LINKS` after a recording of a second chunk and of the body of a link.
- Decided, D193: the driver accepts the three fraction digits of `TIMESTAMP` and `TIMESTAMP_NTZ`. A caller who needs
  microseconds casts to a string in SQL.
- Decided, D193: the DSN is `databricks://token:<pat>@host/<warehouse-id>?catalog=&schema=`, with the token as the
  password (D94). OAuth with a client id and a secret can come later.
- Decided, D193: `BeginTx` returns the error of D20. The driver sends the catalog and the schema of the DSN with every
  statement. `SET`, `USE`, temporary views and variables do not last between statements.
- Decided, D193: several statements go to the server, which answers a parse error. The driver sends `wait_timeout` of
  50 seconds and polls. `Exec` reads the count row, and `RowsAffected` returns the wrapped `dbimp.ErrNotSupported` when
  there is none. The error type exposes the HTTP status, `error_code`, `sql_state` and the message. The driver gives no
  answer to a version request (D181), the token goes only to the configured host, and the driver serves no flavor.
- Decided, D193: the types are as in the type table. `STRUCT` is a map, `INTERVAL` is a `dbimp.Interval`, and a `VARIANT`
  with a JSON null or a SQL NULL gives `nil`.
1. R. Databricks is a hosted service with no emulator, so a test needs a workspace and a token. The Free Edition
   workspace is free and has one warehouse that stops after 10 minutes. Whether a driver here has integration tests in CI
   against a workspace, and with what secret, is a question for Ken (D188, dbmeta D117).
2. The host. `dburl` adds `.cloud.databricks.com` to a host that has no dot. Whether the driver accepts a port other than
   443 and `http` for a test is not decided.
3. The recordings contain presigned link URLs. Their signatures were redacted by the main session.
4. The leads for a second live pass, in Second opinions, stay open.
5. The tests that `features.json` names do not exist yet. The gate that reads them runs when the package `databricks/`
   exists. The type table has a row that the matrix of `TYPES.md` does not have yet, and the test that writes the matrix
   fails until it runs with `DBIMP_UPDATE=1`.
