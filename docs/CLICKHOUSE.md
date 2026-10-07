# ClickHouse

This file holds what is known about ClickHouse over HTTP, for the driver
`clickhouse` (W32 and D174). The headings are the template of
[DRIVER.md](DRIVER.md).

Steps 5a to 8a measured `clickhouse-25.3`, `clickhouse-25.8` and
`clickhouse-26.9` on 2026-10-07, as `default`, the administrator that `dbrun`
names, and as `dbimp_user`, an ordinary user that the setup of `requests.json`
makes (see Principals). A fact marked "recorded" is in `testdata/clickhouse/`,
and the name in quotes after it is the name of its request in `requests.json`
there. The three releases gave the same answers, except where a line names a
release. A fact marked "measured" names how it was measured. A fact marked
"not measured" names its source. The sources, each read on 2026-10-07, are
these:

- "The dbmeta entry" is `container/clickhouse.go` in `dbmeta`, and "the model"
  is `models/clickhouse` there.
- "The wire report" is the report of the `dbmeta` session on the MySQL and
  PostgreSQL ports of ClickHouse, written on 2026-10-07 for D174.
- "dburl" is `scheme.go` and `dsn.go` in `dburl`.
- "usql" is `drivers/clickhouse/clickhouse.go` in `usql`, and its `go.mod`.
- "clickhouse-go" is `github.com/ClickHouse/clickhouse-go` at commit `f0879a5`
  of 2026-10-06, with its issues. `usql` uses v2.48.0.
- "clickhouse-connect" is `github.com/ClickHouse/clickhouse-connect` at commit
  `fec4e57` of 2026-10-06, the official Python client.
- "Gemini" is `gemini-3.1-pro-preview`, and "DeepSeek" is `deepseek-v4-pro`,
  asked on 2026-10-07.
- "The forwarder" is a Go program in the scratch folder of the measurement.
  See the next paragraph.

Before `dbmeta` v0.3.0, `dbrun` published the native port 9000 of each release,
and it did not publish the HTTP port 8123 (measured with `curl` on `clickhouse-25.8` on
2026-10-07: a request to the published port answered HTTP 400 with `Port 9000
is for clickhouse-client program. You must use port 8123 for HTTP.`). The
dbmeta entry says the same in its comment. The `podman ps` of the container
shows `8123/tcp` exposed and not published. So the recorder reached the HTTP
port through the forwarder. The forwarder listens on `127.0.0.1:18123`. For
each connection it runs `podman exec -i <container> bash -c` in the container
that `dbrun` started, and the command opens `/dev/tcp/127.0.0.1/8123` in the
container and copies the bytes both ways. The forwarder starts no container,
and it changes nothing in `dbmeta`. It closes the socket in the container when
the client closes its side, and a test showed that the server then sees the
disconnect (measured with `curl` on `clickhouse-25.8` on 2026-10-07: a query
of `sleepEachRow` that writes rows ended within 3 seconds after the client
left). The first version of the forwarder did not close that socket, and its
recordings of a cancel were wrong, so every file in `testdata/clickhouse/` was
recorded again with the second version. Every recorded exchange is a real
answer of a real server. The paragraph above and the
forwarder describe the situation before `dbmeta` v0.3.0, which now prints the
port 8123, the `api` address and the user `dbmeta_user` (Open questions, 1).
The recordings still hold the user `dbimp_user`, which is true, because Step 6
ran before that release.

## Summary

- ClickHouse is a column store for analytics. A table has an engine, and the
  engine `MergeTree` keeps rows sorted by the key `ORDER BY`. The server
  speaks several protocols, and the HTTP interface is one of them.
- R holds for the server, and it held for the HTTP port from `dbmeta` v0.3.0.
  `dbrun` starts `clickhouse-25.3`, `clickhouse-25.8` and `clickhouse-26.9`
  from the image `docker.io/clickhouse/clickhouse-server` (the dbmeta entry).
  Before `dbmeta` v0.3.0, it published only the native port, and it named no
  ordinary user (measured: `dbrun dsn --json clickhouse-25.8` on 2026-10-07
  printed one principal, the administrator, and no `api` address). Now it
  prints the port 8123, the `api` address and the user `dbmeta_user`. See
  Open questions, 1.
- H and S hold. `POST /` with the SQL in the body answered HTTP 200 as both
  principals on each release (recorded: "a statement in the body"). The SQL is
  the dialect of ClickHouse.
- The priority is in [TARGETS.md](TARGETS.md): ClickHouse is number 17 of the
  order (D174).
- The server reports its version with `SELECT version()`, as `25.3.14.14`,
  `25.8.33.6` and `26.9.2.8` (recorded: "the version").
- The four statements of CRUD work, and update has two forms. `INSERT`,
  `SELECT`, `ALTER TABLE ... UPDATE` and `DELETE FROM ... WHERE` answered HTTP
  200 on each release (recorded: "crud: insert values", "crud: select after
  insert", "crud: update with a mutation" and "crud: delete, the lightweight
  form"). The statement `UPDATE ... SET` answered HTTP 501 on a table that has
  no `_block_number` column, on 25.8 and 26.9, and a syntax error on 25.3
  (recorded: "crud: update, the lightweight form"). It worked on a table made
  with `enable_block_number_column = 1` on 25.8 and 26.9 (recorded: "crud:
  lightweight update on a table with the block number column"). DRIVER.md says
  to ask Ken when a server refuses one of the four statements. See Open
  questions.
- A mutation (`ALTER TABLE ... UPDATE` and `ALTER TABLE ... DELETE`) runs in
  the background, and the setting `mutations_sync=2` makes the request wait
  for it (recorded: "crud: update with a mutation").
- There is no upsert, no `MERGE`, no foreign key and no unique constraint
  (recorded: "crud: upsert", "crud: merge", "schema: a foreign key with alter"
  and "schema: a unique constraint").
- There are no transactions. `BEGIN` answers HTTP 501 (recorded: "begin a
  transaction").
- A result is not paged. The server sends every row in one response (recorded:
  "a result of 5000 rows").
- The format decides the driver, and the next section of Responses holds the
  facts. `JSONCompactEachRowWithNamesAndTypes` and
  `RowBinaryWithNamesAndTypes` both name the columns and the exact types
  before the first row, and both stream. JSON loses some values, and RowBinary
  keeps all of them.
- `dburl` has the scheme `clickhouse`, with the generator `GenClickhouse`, the
  alias `ch` and the dialect `clickhouse`. Its `GoPackage` is
  `github.com/ClickHouse/clickhouse-go/v2`. The generator writes
  `clickhouse://localhost:9000/` for the transport `tcp` or none,
  `http://localhost:8123/` for `http` and `https://localhost:8443/` for
  `https` (dburl, `scheme.go` and `dsn.go`).
- `usql` has a driver, and it uses `clickhouse-go/v2` v2.48.0. That driver
  speaks the native protocol by default and has an HTTP mode that reads the
  format `Native` (clickhouse-go, `conn_http.go`). The file of `usql` sets
  `RowsAffected` to return 0, maps the exception code 516 to a password error,
  and sets no `Version` statement (usql).
- `dbmeta` has a model for ClickHouse. It reads the version with `SELECT
  version()` (the model, `clickhouse.go`). The entry puts 25.8 and 26.9 in the
  tier Tested, and 25.3 and 26.8 in the tier Nightly, and it starts the image
  with `CLICKHOUSE_PASSWORD` and `CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1` (the
  dbmeta entry).
- The wire report says that the MySQL and PostgreSQL ports lose the type, the
  zone, transactions and bound parameters. Ken decided on 2026-10-07 that the
  HTTP driver goes ahead (D174).

## Requests

These facts were recorded on each release, with the format
`JSONCompactEachRowWithNamesAndTypes` unless a line names another:

- A statement is `POST /` with the SQL as the body. The headers `Content-Type:
  text/plain` and `Content-Type: application/json` change nothing, and neither
  does the absence of a content type (recorded: "a statement in the body", "a
  statement in the body with a plain text content type" and "a statement in
  the body with a JSON content type").
- A statement can be the value of the query key `query`, with GET or POST
  (recorded: "a statement in the query string by GET"). With POST, the server
  joins the text of `query` and the text of the body, and the join was a
  syntax error for two complete statements (recorded: "a statement in the
  query string by POST"). The server has no field of a form: a body of
  `query=SELECT+3+AS+a` is SQL and a syntax error (recorded: "a statement as a
  form field"). A request with the `multipart/form-data` encoding carries the
  fields `query` and `param_<name>` and the files of external tables
  (recorded: "a multipart form with the query and a parameter", "a multipart
  form with an external table" and "a multipart form with an external table
  and a parameter").
- An empty body is HTTP 400 `Empty query` (recorded: "a statement with an
  empty body").
- GET makes the request readonly. A `CREATE TABLE` with GET answered HTTP 500
  and the error `Cannot execute query in readonly mode. For queries over HTTP,
  method GET implies readonly` (recorded: "a GET that writes"). A request with
  PUT or DELETE answered HTTP 501 (recorded: "a PUT request" and "a DELETE
  request").
- The format comes from the query key `default_format`, or from the request
  header `X-ClickHouse-Format`, or from a `FORMAT` clause at the end of the
  statement. The clause wins over the key (recorded: "a request with the
  header X-ClickHouse-Format" and "a statement with the FORMAT clause"). With
  none of the three, the format is `TabSeparated` (recorded: "a statement with
  no default format").
- The database comes from the query key `database` or from the request header
  `X-ClickHouse-Database`. An unknown database is HTTP 404 with the code 81
  (recorded: "the database in the header X-ClickHouse-Database", "the database
  in the query string", "a database in the query string that does not exist"
  and "a database in the header that does not exist").
- A setting goes in the query string, such as `max_threads=3` (recorded:
  "feature: the settings in the query string"). The request header
  `X-ClickHouse-Setting-max_threads` changed nothing: the setting kept its
  default of 32 (recorded: "feature: the settings in the header"). A setting
  that the server does not know is HTTP 404 with the code 115 (recorded: "an
  error before rows, an unknown setting"). The statement can end with a
  `SETTINGS` clause (recorded: "a statement with a settings clause").
- Authentication is HTTP Basic in every recorded request. A request that also
  sent `X-ClickHouse-User` or `X-ClickHouse-Key` with the header of Basic
  answered HTTP 403 and the code 516, `it is not allowed to use X-ClickHouse
  HTTP headers and Authorization HTTP header simultaneously` (recorded: "the
  user in the header X-ClickHouse-User with the header of Authorization", "the
  key in the header X-ClickHouse-Key with the header of Authorization" and
  "the user and the key in the headers with the header of Authorization"). A
  request with the query key `user` and the header of Basic answered HTTP 403
  and the code 516 on 25.3 and 25.8, for both users. On 26.9 it answered HTTP
  401 and the code 194 for the administrator, and HTTP 403 and the code 516
  for the ordinary user (recorded: "the user in the query string with the
  header of Authorization").
- Each of three other ways of sending the credentials worked on each release
  (measured with `curl` on 2026-10-07): the headers `X-ClickHouse-User` and
  `X-ClickHouse-Key` with no `Authorization`, and the query keys `user` and
  `password`. The header `Authorization: Bearer abc` answered HTTP 403 with
  `'Bearer' HTTP Authorization scheme is not supported`, on each release
  (measured with `curl` on 2026-10-07).
- The header `Expect: 100-continue` changed nothing (recorded: "a request with
  the header Expect"). The header `X-ClickHouse-Quota` and a role header
  `X-ClickHouse-Default-Roles` were accepted and had no visible effect on 25.3
  (recorded: "a request with the header X-ClickHouse-Quota", and measured with
  `curl` on `clickhouse-25.3` on 2026-10-07 for the role header). The query
  key `role` named a role, and an unknown role was HTTP 404 with the code 511
  (recorded: "a role in the query string").
- A request header `X-ClickHouse-Query-Id` gave the query its id, and the
  answer repeated it (recorded: "a request with the header
  X-ClickHouse-Query-Id"). So did the query key `query_id` (recorded: "the
  query_id in the response").
- The endpoints are `GET /ping` and `GET /` and `GET /replicas_status`, which
  answer `Ok.` (recorded: "the ping endpoint", "the root endpoint" and "the
  replicas status endpoint"), `HEAD /ping` with HTTP 200 and `OPTIONS /` with
  HTTP 204 (recorded: "a HEAD request" and "an OPTIONS request"). A path that
  does not exist is HTTP 404 with a text that names `/` and `/ping` (recorded:
  "a request to a path that does not exist"). `GET /play` answered an HTML
  page of about 1 MB (measured with `curl` on `clickhouse-25.8` on
  2026-10-07). The recorder kept no such page, because of its size.
- Each answer has the headers `X-ClickHouse-Query-Id`, `X-ClickHouse-Summary`,
  `X-ClickHouse-Format`, `X-ClickHouse-Timezone` and
  `X-ClickHouse-Server-Display-Name` (recorded: "the headers of a response of
  a select"). `X-ClickHouse-Timezone` follows the setting `session_timezone`
  (recorded: "the header X-ClickHouse-Format and the timezone").
  `X-ClickHouse-Summary` is a JSON object of text values, with `read_rows`,
  `written_rows` and others (recorded: "the summary of an insert" and "a
  response with the summary of a select"). An `INSERT` with `VALUES` wrote
  `written_rows` 1, and a `DELETE`, an `ALTER ... UPDATE` and a `TRUNCATE`
  wrote 0 (recorded: "the summary of an insert", "crud: delete, the
  lightweight form", "crud: update with a mutation" and "crud: truncate"). An
  insert with `async_insert=1` wrote 0 on 25.3 and 25.8 and 1 on 26.9
  (recorded: "crud: insert with the setting async_insert").
- The content type of the answer follows the format. `JSONCompactEachRowWithNamesAndTypes` is `text/plain; charset=UTF-8`, `JSON` and `JSONCompact` are `application/json`, `JSONEachRow` is `application/x-ndjson`, `TabSeparated` is `text/tab-separated-values`, and `RowBinary`, `Native`, `Parquet` and `ArrowStream` are `application/octet-stream` (recorded: "the headers of a response of a select", "columns, JSON", "columns, JSONEachRow", "a statement with no default format", "columns, RowBinaryWithNamesAndTypes" and "columns, Native").

These facts come from the sources and are not measured:

- clickhouse-go in HTTP mode sets `default_format=Native` on each request and
  reads columnar blocks (clickhouse-go, `conn_http.go`). It sends the user and
  the password in `X-ClickHouse-User` and `X-ClickHouse-Key`. It sends a
  parameter as `param_<name>` (clickhouse-go, `conn_http.go`).
- clickhouse-connect writes `FORMAT Native` in its inserts, sends the keys
  `session_id`, `query_id` and `wait_end_of_query`, and the headers
  `X-ClickHouse-User` and `X-ClickHouse-SSL-Certificate-Auth`
  (clickhouse-connect, `driver/httpclient.py`).
- Gemini and DeepSeek named the header `X-ClickHouse-User` and
  `X-ClickHouse-Key`, the query key `database`, the header
  `X-ClickHouse-Database`, `query_id` and `session_id`. See Second opinions.

## The DSN

- `dburl` writes `http://localhost:8123/` and `https://localhost:8443/` for
  the HTTP transports of the scheme `clickhouse` (dburl, `dsn.go`). The ports
  8123 for HTTP and 8443 for HTTPS are the defaults of the server (the
  documentation of ClickHouse, not measured). TLS was not measured, because
  the image of `dbrun` has no certificate. A request to `https://` on the
  forwarded HTTP port got no answer (measured with `curl` on `clickhouse-25.8`
  on 2026-10-07).
- `dbrun` gives the administrator as
  `clickhouse://default:P4ssw0rd%21x@127.0.0.1:<port>/default`, the address of
  the native port, and it gives no `api` address (measured: `dbrun dsn --json`
  on 2026-10-07). The path of that URL is the database `default`. The dbmeta
  entry builds it.
- The database is a query key or a header on each request, and the path of a
  URL is the usual place to name it (see Requests).
- clickhouse-go reads the keys `secure`, `skip_verify`, `compress`,
  `http_proxy`, `http_path` and others from a DSN, and the scheme `http` or
  `https` selects its HTTP mode (clickhouse-go, `clickhouse_options.go`). It
  registers the name `clickhouse` with `database/sql` (clickhouse-go,
  `clickhouse_std.go`). So the driver here and clickhouse-go cannot be linked
  into one program, because `database/sql` refuses a name twice.
- The recorder takes the credentials from the URL of each principal and sends
  them as a Basic header.

## Responses

These facts were recorded on each release:

- The answer streams. It has the header `Transfer-Encoding: chunked`, and a
  result of 5000 rows was one response (recorded: "a result of 5000 rows").
  The server has no paging, no cursor and no `nextUri`.
- A result has no default cap. A result of 20000 rows came back whole
  (recorded: "a result of 20000 rows from numbers").
- The settings `max_result_rows` and `max_result_bytes` cap a result, with
  `result_overflow_mode`. The mode `throw` ended the result with the code 396
  and HTTP 500 (recorded: "a result with max_result_rows and throw" and "a
  result with max_result_bytes and throw"). The mode `break` stops at the end
  of a block, so a cap of 100 rows still returned all 5000 rows, which were
  one block (recorded: "a result with max_result_rows and break"). The mode
  `break` of `max_execution_time` ended the result with no error after the
  first row (recorded: "max_execution_time with break").
- `LIMIT` and `OFFSET` work (recorded: "a result with a limit").
- The setting `send_progress_in_http_headers=1` adds the header
  `X-ClickHouse-Progress` to the answer, once for each report, and
  `http_headers_progress_interval_ms` sets the interval (recorded: "a result
  with the progress in headers").
- The setting `wait_end_of_query=1` makes the server hold the whole result and
  send the status last. An error after some rows was then HTTP 500 with the
  text of the error and no rows (recorded: "an error after rows, with
  wait_end_of_query"). The setting `buffer_size` sets the size of that buffer
  (recorded: "a result with a buffer size").
- The headers of the answer do not hold the names of the columns. Each format
  holds them in its body.
- Compression of the answer needs `Accept-Encoding` and the setting
  `enable_http_compression=1`. On 25.3 and 25.8 a request with
  `Accept-Encoding: gzip` and no setting got a plain body. On 26.9 the same
  request got a gzip body, because the setting is 1 by default there
  (recorded: "a response with gzip asked for and compression off", and
  measured with `curl` on `clickhouse-26.9` on 2026-10-07: `system.settings`).
  With the setting on, the server answered with `gzip`, `deflate`, `zstd`,
  `br` and `xz` (recorded: "a response compressed with gzip", "a response
  compressed with deflate", "a response compressed with zstd", "a response
  compressed with brotli" and "a response compressed with xz"). An encoding
  that the server does not know was ignored and the body was plain (recorded:
  "a response compressed with an encoding that does not exist"). An error
  answer was compressed too (recorded: "a response compressed with gzip and an
  error"). The format `Native` with `compress=1` writes its blocks in a
  compressed form of its own (recorded: "a response of the native
  compression").
- A request with `Content-Encoding: gzip` and a body that is not gzip answered
  HTTP 500 with the code 354 on 25.3 and 25.8 and the code 271 on 26.9, and an unknown encoding answered HTTP 501 with
  the code 48 (recorded: "a request with a body that is not compressed but
  says it is" and "a request with an unknown content encoding"). A gzip body
  worked on each release (measured with `curl` on 2026-10-07).
- The server sent no redirect to any request of the recording.
  `max_http_get_redirects` is 0 on 26.9 and concerns the requests that the
  server sends, not the ones that it receives (measured: `system.settings` on
  26.9 on 2026-10-07).
- The header `Keep-Alive` says `timeout=10` on 25.3 and 25.8 and `timeout=30`
  on 26.9, with `max=9999` (recorded: "the headers of a response of a
  select"). The header `Connection: Keep-Alive` is on every answer. A request
  with `Connection: close` worked (recorded: "a request with the header
  Connection close").
- 26.9 sends the header `X-ClickHouse-Exception-Tag` on every answer, with a
  random text of 16 letters, and the earlier releases do not (recorded: "the
  headers of a response of a select").

### The formats

Each of these formats answered the same statement with the columns before the
first row. The statement was `SELECT 1 AS a, 'x' AS b, NULL AS c, toInt64(-2)
AS d, 1 AS a`, which has a repeated name (recorded: "columns,
JSONCompactEachRowWithNamesAndTypes" and the requests whose names start with the word columns):

- `JSONCompactEachRowWithNamesAndTypes` writes one JSON array of strings with
  the names, one JSON array of strings with the exact type names, and then one
  JSON array for each row, each on its own line. It writes the two lines for a
  result with no rows. It keeps a repeated name and the order of the columns.
  A `NULL` literal has the type `Nullable(Nothing)`.
- `JSONCompactEachRowWithNames` has the names and no types.
  `JSONCompactEachRow` has neither.
- `JSONCompact` and `JSON` write one object with `meta` (the names and the
  types), `data`, `rows`, optionally `totals`, `extremes` and
  `rows_before_limit_at_least`, and `statistics`, with the tail after the rows
  (recorded: "a result with a totals row" and "a result with extremes"). A
  result with no rows has `meta` and an empty `data` (recorded: "no rows,
  JSONCompact").
- `JSONEachRow` writes one object for each row, and a repeated name gives a
  repeated key, so it is not a safe format for the columns (recorded:
  "columns, JSONEachRow").
- `JSONCompactStringsEachRowWithNamesAndTypes` writes every value as a string,
  and writes `ᴺᵁᴸᴸ` for a NULL (recorded: "columns,
  JSONCompactStringsEachRowWithNamesAndTypes").
- `RowBinaryWithNamesAndTypes` writes a varint with the number of columns,
  each name as a varint length and bytes, each type name the same way, and
  then the values of each row with no separator. `RowBinaryWithNames` has no
  types. A result with no rows has the names and the types (recorded:
  "columns, RowBinaryWithNamesAndTypes", "columns, RowBinaryWithNames" and "no
  rows, RowBinaryWithNamesAndTypes").
- `Native` writes columnar blocks, and a result with no rows is an empty body
  (recorded: "columns, Native" and "no rows, Native"). `ArrowStream`,
  `Parquet`, `Avro`, `Npy`, `CSV`, `TabSeparatedWithNamesAndTypes`,
  `Vertical`, `PrettyCompact`, `Markdown` and `XML` answered too (recorded:
  "columns, Arrow", "feature: format Parquet", "feature: format Avro",
  "feature: format Npy", "feature: format CSV", "columns,
  TabSeparatedWithNamesAndTypes", "feature: format Vertical", "feature: format
  Pretty", "feature: format Markdown" and "feature: format XML"). The format
  `Template` answered HTTP 500 for lack of a template, `RowBinaryWithDefaults`
  answered HTTP 500 because it is not a format of output, and an unknown
  format answered HTTP 404 with the code 73 (recorded: "feature: format
  Template", "feature: format RowBinaryWithDefaults" and "an error before
  rows, an unknown format").
- A statement with no result, such as an `INSERT` or a `CREATE TABLE`, answers
  HTTP 200 with an empty body, in every format (recorded: "a statement that
  returns no columns" and "a statement that returns no columns, JSONCompact").
  The number of rows that an `INSERT` wrote is in `X-ClickHouse-Summary` (see
  Requests).

These are the facts that decide between the two streaming formats. Each fact
is in the table of Types for each type, and here is the sum:

- Both formats arrive row by row, and a decoder of each reads the next row
  only when the caller asks for it.
- `JSONCompactEachRowWithNamesAndTypes` is text that `encoding/json/jsontext`
  reads one token at a time. The 64-bit and wider integers, the decimals and
  the floats are JSON numbers, so a decoder that keeps each number as text
  loses no digit. On 25.3 the default quotes every integer of 64 bits or more
  as a JSON string, and the setting `output_format_json_quote_64bit_integers`
  turns that on or off on each release (recorded: "every type,
  JSONCompactEachRowWithNamesAndTypes", "every type,
  JSONCompactEachRowWithNamesAndTypes with every quote setting" and "every
  type, JSONCompactEachRowWithNamesAndTypes with no quote setting"). A NaN and
  an infinity are `null` unless `output_format_json_quote_denormals=1` writes
  `"nan"`, `"inf"` and `"-inf"` (recorded: "NaN and infinities by default" and
  "NaN and infinities, quoted"). A string with bytes that are not UTF-8 is
  written as it is, so the line is not valid JSON, and `jsontext` refuses it
  unless the decoder allows invalid UTF-8 (measured: a program of the scratch
  folder, with Go 1.27.1 on 2026-10-07). With `AllowInvalidUTF8` the raw value
  keeps the bytes, and a token gives U+FFFD in their place. The setting
  `output_format_json_validate_utf8=1` writes U+FFFD in place of the bytes
  (recorded: "a string of invalid UTF-8" and "a string of invalid UTF-8,
  validated"). A time of `DateTime` and `DateTime64` is text in the zone of
  the column, with no offset, and the zone is only in the type name. A time in
  the hour of the autumn change is not unique: `2026-11-01 01:30:00.000` in
  `America/New_York` is the text of two instants, one hour apart (measured
  with `curl` on `clickhouse-26.9` on 2026-10-07: the epoch values
  1793511000000 and 1793514600000 gave the same text). The setting
  `date_time_output_format=iso` writes the instant in UTC, such as
  `2026-11-01T05:30:00.000Z`, and the zone of the type name is then the only
  place of the zone (recorded: "a date time with each output format, iso").
  The value `unix_timestamp` writes a number, and wrote `0.00/` for the time
  `1969-12-31 23:59:59.999` of `DateTime64(3)` on each release (recorded: "a
  date time with each output format, unix_timestamp"). A `Variant` and a
  `Dynamic` are the JSON of the value, and the member type is lost. A column
  of the type `JSON` is an object, and on 25.3 each integer in it is a string
  (recorded: "a JSON column", and "a dynamic").
- `RowBinaryWithNamesAndTypes` gives each value in its type. An integer of up to 256 bits is little endian bytes, and a float is its bits, with a NaN and an infinity. A `String` is its bytes, with any value. A `DateTime64` is the number of ticks since the epoch, and an `Enum` is its number. A `Variant` is the index of its member and the value, and a `Dynamic` is a binary type name and the value. A decoder of the recorded body of `dbimp_types` that I wrote in
  Python in the scratch folder read all three rows of every column, and the
  values matched those of the JSON (measured on 2026-10-07 with the recorded
  bodies of 25.8). It read the largest `UInt256`, the smallest `Int256`, the invalid bytes `ff80` and the NaN of the third row. It read the epoch value of the time in the hour of the autumn change, and the member index of a `Variant`. It read the type of each value of a `Dynamic` and of each path of a `JSON`. It needed
  one hand written rule for the state of an `AggregateFunction`, which has no
  length in the format. So no generic decoder can skip or read that type
  (recorded: "an aggregate function state, RowBinary"). clickhouse-connect
  treats `AggregateFunction` as a type that it does not support
  (clickhouse-connect, `datatypes/special.py`).
- A column of the type `JSON` in `RowBinary` is a count of paths, and then
  each path name and a `Dynamic` value, unless the setting
  `output_format_binary_write_json_as_string=1` writes the text of the object
  as a string (measured with `curl` on `clickhouse-26.9` on 2026-10-07, and
  recorded: "a JSON column, RowBinary").
- A mid-stream error after some rows is the same problem in both formats (see
  Errors).
- A size estimate of a `RowBinaryWithNamesAndTypes` decoder in Go is not
  measured. The Python decoder had about 190 lines, with a type parser, the
  scalar types, the composite types, `Variant`, `Dynamic` and `JSON`. A Go
  decoder with the same parts is about 1,200 lines, and a decoder of the JSON
  form that keeps each number as text is about 500 lines (an estimate from the
  Python decoder and from the other drivers of this module). Both need a
  parser of the type names, such as `Decimal(76, 40)` and `DateTime64(3,
  'America/New_York')`. Ken decides the format at step 9, and a binary format
  needs his approval (D13).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table for
Ken to review. The wire type is the family of the type name that the server
writes in the second line of the format `JSONCompactEachRowWithNamesAndTypes`,
without its arguments, such as `Decimal` for `Decimal(18, 4)`. A name that
starts with a release is a type that only that release has. The types
`Nullable`, `LowCardinality`, `SimpleAggregateFunction` and `Nested` are
wrappers and have no row: the column takes the Go type of the type inside, and
a `Nullable` column can hold a NULL (recorded: "a nullable column from a table
has no default marker", "a low cardinality type" and "a column of a table
named with a dot"). The Go type of a decimal needs the arguments of the type
name, because the scale is only there.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| Nothing | null | `nil` | `interface {}` | `NOTHING` | yes |
| Bool | boolean | `bool` | `bool` | `BOOL` | yes |
| Int8 | integer | `int64` | `int64` | `INT8` | yes |
| Int16 | integer | `int64` | `int64` | `INT16` | yes |
| Int32 | integer | `int64` | `int64` | `INT32` | yes |
| Int64 | integer | `int64` | `int64` | `INT64` | yes |
| UInt8 | integer | `int64` | `int64` | `UINT8` | yes |
| UInt16 | integer | `int64` | `int64` | `UINT16` | yes |
| UInt32 | integer | `int64` | `int64` | `UINT32` | yes |
| UInt64 | unsigned integer | `uint64` | `uint64` | `UINT64` | yes |
| Int128 | big integer | `*big.Int` | `*big.Int` | `INT128` | yes |
| Int256 | big integer | `*big.Int` | `*big.Int` | `INT256` | yes |
| UInt128 | big integer | `*big.Int` | `*big.Int` | `UINT128` | yes |
| UInt256 | big integer | `*big.Int` | `*big.Int` | `UINT256` | yes |
| Float32 | float | `float64` | `float64` | `FLOAT32` | yes |
| Float64 | float | `float64` | `float64` | `FLOAT64` | yes |
| BFloat16 | float | `float64` | `float64` | `BFLOAT16` | yes |
| Decimal | decimal | `*apd.Decimal` | `*apd.Decimal` | `DECIMAL` | yes |
| String | string | `string` | `string` | `STRING` | yes |
| FixedString | string | `string` | `string` | `FIXEDSTRING` | yes |
| Enum8 | string | `string` | `string` | `ENUM8` | yes |
| Enum16 | string | `string` | `string` | `ENUM16` | yes |
| Date | date | `dbimp.Date` | `dbimp.Date` | `DATE` | yes |
| Date32 | date | `dbimp.Date` | `dbimp.Date` | `DATE32` | yes |
| DateTime | timestamp | `time.Time` | `time.Time` | `DATETIME` | yes |
| DateTime64 | timestamp | `time.Time` | `time.Time` | `DATETIME64` | yes |
| Time | duration | `time.Duration` | `time.Duration` | `TIME` | yes |
| Time64 | duration | `time.Duration` | `time.Duration` | `TIME64` | yes |
| Interval | interval | `dbimp.Interval` | `dbimp.Interval` | `INTERVAL` | yes |
| UUID | uuid | `uuid.UUID` | `uuid.UUID` | `UUID` | yes |
| IPv4 | ip address | `netip.Addr` | `netip.Addr` | `IPV4` | yes |
| IPv6 | ip address | `netip.Addr` | `netip.Addr` | `IPV6` | yes |
| Array | array | `[]any` | `[]interface {}` | `ARRAY` | no |
| Tuple | tuple | `[]any` | `[]interface {}` | `TUPLE` | yes |
| Map | map | `map[string]any` | `map[string]interface {}` | `MAP` | no |
| Variant | json | `the decoded JSON value` | `interface {}` | `VARIANT` | yes |
| Dynamic | json | `the decoded JSON value` | `interface {}` | `DYNAMIC` | yes |
| JSON | json | `the decoded JSON value` | `interface {}` | `JSON` | yes |
| Point | geometry | `[]any` | `[]interface {}` | `POINT` | yes |
| Ring | geometry | `[]any` | `[]interface {}` | `RING` | no |
| Polygon | geometry | `[]any` | `[]interface {}` | `POLYGON` | no |
| MultiPolygon | geometry | `[]any` | `[]interface {}` | `MULTIPOLYGON` | no |
| LineString | geometry | `[]any` | `[]interface {}` | `LINESTRING` | no |
| MultiLineString | geometry | `[]any` | `[]interface {}` | `MULTILINESTRING` | no |
| 26.9 MultiPoint | geometry | `[]any` | `[]interface {}` | `MULTIPOINT` | no |
| 26.9 Geometry | geometry | `[]any` | `[]interface {}` | `GEOMETRY` | no |
| AggregateFunction | binary | `[]byte` | `[]uint8` | `AGGREGATEFUNCTION` | no |
| 26.9 QBit | vector | `dbimp.Vector[float32]` | `dbimp.Vector[float32]` | `QBIT` | yes |
<!-- /dbimp:types -->

The Go type of a row that is a pointer or a package type is the one that
TYPES.md names for its kind. The mapping gives a Go type that differs from the
Go type of its kind, or that fits no kind, for these types (the reasons are
facts, and the choice is for step 9):

- `Int128`, `Int256`, `UInt128` and `UInt256` are integers that `int64` and
  `uint64` cannot hold. The table gives them the kind decimal and
  `*apd.Decimal` with the scale 0, because Go has no wider integer and `apd`
  is already in the module (D33). A new kind "big integer" with `*big.Int` is
  the other choice.
- `Time` and `Time64` are signed and reach 999 hours, so they do not fit the
  kind "time of day", whose hour is 0 to 23 (D138). The table gives them the
  kind duration and `time.Duration`. The range of `Time64(9)` fits an `int64`
  of nanoseconds (999:59:59 is about 3.6e15 ns).
- `IPv4` and `IPv6` fit no kind. The table gives them the kind other, which is
  the text of the server, as Trino does for `IPADDRESS`. A new kind "ip
  address" with `netip.Addr` is the other choice.
- `FixedString` and `String` hold any bytes, and the table gives them the kind
  string. The kind binary and `[]byte` is the other choice, for the reason
  that invalid UTF-8 is possible.
- `Interval` has 11 type names, from `IntervalNanosecond` to `IntervalYear`. A
  value is a number of one unit, and the table gives it the kind interval with
  `dbimp.Interval`: a year is 12 months, a quarter is 3 months, a week is 7
  days, and the units below a day are nanoseconds.
- `Point`, `Ring`, `Polygon`, `MultiPolygon`, `LineString`, `MultiLineString`,
  `MultiPoint` and `Geometry` are the kind geometry. D136 lets each driver
  choose the form. The table gives nested `[]any` of `float64`, which is the
  JSON that the server writes. WKT and GeoJSON are the other choices.
- `Variant`, `Dynamic` and `JSON` are the kind json. The JSON does not say
  which member of a `Variant` or which type of a `Dynamic` a value has.
- `AggregateFunction` is the kind binary. It is the state of an aggregate
  function, and its bytes have no meaning to a caller.
- `Float32` is the kind float and `float64`. The text of a `Float32` is the
  shortest text that reads back as that `Float32`, such as `0.1`, and the
  `float64` of that text is 0.1 and not the 0.10000000149011612 of the
  `float32` value (measured with `curl` on `clickhouse-26.9` on 2026-10-07).
- `Date` and `DateTime` have no kind that is a local timestamp. `DateTime` and
  `DateTime64` are instants in the zone of the column, so the table gives them
  the kind timestamp and `time.Time`. This needs the instant, which only the
  setting `date_time_output_format=iso` or the format `RowBinary` gives. See
  Second opinions.

These facts were recorded on each release, from the table `dbimp_types` with
three rows: the largest values, the zero values and empty values, and a row
with the smallest values and NULLs (recorded: "every type,
JSONCompactEachRowWithNamesAndTypes", unless a line says otherwise):

- A `NULL` is `null` in a `Nullable` column, in every JSON format. In
  `JSONCompactStringsEachRowWithNamesAndTypes` it is the text `ᴺᵁᴸᴸ`, and in
  `TabSeparated` it is `\N` (recorded: "columns,
  JSONCompactStringsEachRowWithNamesAndTypes" and "columns,
  TabSeparatedWithNamesAndTypes"). A column that is not `Nullable` holds no
  NULL. A row that leaves out the column gets the default of its type, such as
  0 and the empty string and `1970-01-01 00:00:00` (recorded: "crud: select
  the default"). An empty string and a NULL are two values in a
  `Nullable(String)` (recorded: "a nullable column from a table has no default
  marker"). There is no value that is missing.
- These types cannot be inside `Nullable`: `Array`, `Map`, `Variant`,
  `Dynamic` and `AggregateFunction` on each release, and `Ring`, `Polygon`,
  `MultiPolygon`, `LineString`, `MultiLineString`, `MultiPoint` and
  `Geometry`. `Tuple` and `Point` can be on 26.9 and cannot be on 25.3 and
  25.8. The error is the code 43 (recorded: "a nullable array", "a nullable
  tuple", "a nullable map", "a nullable variant", "a nullable dynamic", "a
  nullable JSON", "a nullable geo type" and "a nullable aggregate function").
  `LowCardinality` of a `Nullable` number is refused by default with the code
  455 (recorded: "a nullable low cardinality of a number"). `Interval`,
  `Time`, `Time64`, `QBit` and `Int256` can be `Nullable` on 26.9, and
  `MultiPoint`, `Geometry`, `Ring`, `Polygon`, `MultiPolygon`, `LineString`
  and `MultiLineString` cannot (measured with `curl` on `clickhouse-26.9` on
  2026-10-07).
- Each of `Int8`, `Int16`, `Int32` and `UInt8`, `UInt16`, `UInt32` is a JSON
  number. The smallest and the largest values were exact. An `Int64`, a
  `UInt64`, an `Int128`, an `Int256`, a `UInt128` and a `UInt256` are JSON
  numbers with every digit on 25.8 and 26.9, such as 18446744073709551615 and
  115792089237316195423570985008687907853269984665640564039457584007913129639935.
  On 25.3 they are JSON strings by default. The setting
  `output_format_json_quote_64bit_integers` gives each form on each release
  (recorded: "every type, JSONCompactEachRowWithNamesAndTypes with every quote
  setting" and "every type, JSONCompactEachRowWithNamesAndTypes with no quote
  setting"). The Python decoder read the same values from `RowBinary` (see
  Responses).
- An integer that overflows wraps with no error: `toInt8(200)` is -56, and
  `toInt64('9223372036854775808')` is -9223372036854775808. A literal above
  `UInt64` or below `Int64` is a `Float64` and loses digits (recorded: "an
  integer that overflows" and "an integer literal out of range").
- A `Float32` and a `Float64` are JSON numbers with the shortest text that
  reads back, and -0 is `-0`. A NaN and an infinity are `null`, or the strings
  `"nan"`, `"inf"` and `"-inf"` with `output_format_json_quote_denormals=1`
  (recorded: "NaN and infinities by default", "NaN and infinities, quoted" and
  "NaN and infinities in JSONCompact"). The server read a `Float` text at the
  limits differently by release: `toFloat32('3.4028234663852886e38')` gave a
  value one step below the largest `Float32` on 25.3 and 25.8, with the text
  `3.4028233e38`, and the largest on 26.9, with the text `3.4028235e38`, and
  `toFloat64('5e-324')` gave 0 on 25.3 and 25.8 and `5e-324` on 26.9
  (recorded: "a float with its shortest text"). A `BFloat16` is a JSON number
  with the value that it holds, such as 3.140625 for 3.14159 (recorded: "type
  BFloat16").
- A `Decimal` is a JSON number with every digit, with no trailing zeros, so
  1.50 of `Decimal(18, 4)` is `1.5` and 100.00 is `100`. The precision goes up
  to 76 digits, and 77 digits are refused with the code 69 (recorded: "a
  decimal with trailing zeros", "every type,
  JSONCompactEachRowWithNamesAndTypes" and "the longest decimals"). With
  `output_format_json_quote_decimals=1` it is a JSON string (recorded: "a
  decimal as quoted and as unquoted").
- A `Bool` is a JSON `true` or `false` (recorded: "a boolean").
- A `String` is a JSON string that holds any bytes. A control character is
  written as `\u0001`, a `/` as `\/`, and U+2028 as `\u2028` (recorded: "a
  string with escapes" and "a string with each byte"). A byte string that is
  not UTF-8 is written as it is (recorded: "a string of invalid UTF-8"). A
  `FixedString(N)` is padded to N bytes with zero bytes, written as `\u0000`
  (recorded: "a fixed string with zero bytes").
- A `Date` has the range 1970-01-01 to 2149-06-06, and a `Date32` the range
  1900-01-01 to 2299-12-31, as `"YYYY-MM-DD"`. A date outside the range is
  held at the limit with no error, except that 26.9 kept 2300-01-01 in a
  `Date32` (recorded: "a date out of range" and "the ranges of the dates").
- A `DateTime` has the range 1970-01-01 00:00:00 to 2106-02-07 06:28:15, and a
  `DateTime64(P)` writes `P` digits of fraction, up to 9 digits, to 2262-04-11
  23:47:16.854775807. The text has no offset, and it is in the zone of the
  type name, such as `DateTime('Asia/Jakarta')`, or in the zone of the session
  or the server when the type names none (recorded: "dates and times as the
  server writes them", "a date time with a zone from the type" and "the time
  zone of the session"). A time in the hour of the autumn change is not unique
  in the text (see Responses).
- An `Enum8` and an `Enum16` are the name of the member, as a JSON string,
  with the members in the type name, such as `Enum8('a' = 1, 'b' = 2)`. A
  number with no member is an error after the first line on 25.3 and 25.8,
  with HTTP 200, and HTTP 500 on 26.9 (recorded: "an enum with a value outside
  the names" and "an enum").
- A `UUID`, an `IPv4` and an `IPv6` are JSON strings. An `IPv4` mapped into an
  `IPv6` is `::ffff:1.2.3.4` (recorded: "a UUID and its zero" and "an IP
  address").
- An `Array` is a JSON array, and a nested array nests. The empty array has
  the type `Array(Nothing)` (recorded: "a nested array and a tuple in an
  array" and "the type Nothing and NULL"). A `Tuple` with no names is a JSON
  array. A `Tuple` with names is a JSON object, unless
  `output_format_json_named_tuples_as_objects=0` makes it an array (recorded:
  "a tuple with names and with no names" and "a named tuple as an array"). A
  `Map` is a JSON object, and a key of another type is its text: `{"1":"a"}`
  and `{"(1,2)":3}` (recorded: "a map with each key type").
- A `Nested` column is two columns `nest.a` and `nest.b` of the type `Array`
  (recorded: "a column of a table named with a dot").
- A `Variant` and a `Dynamic` are the JSON of their value, so an `Int32` and a `UInt64` are both a bare JSON number and the member type is lost. The functions
  `variantType` and `dynamicType` give it in a second column (recorded: "a
  variant" and "a dynamic"). An element of an `Array(Dynamic)` is the same.
- A `JSON` column is a JSON object, with each path nested. An integer in it is
  a number on 25.8 and 26.9 and a string on 25.3 (recorded: "a JSON column"
  and "every type, JSONCompactEachRowWithNamesAndTypes"). A path with the
  value `null` is left out: `{"a":1,"d":null}` came back with no `d`. A number
  of more than 64 bits is a string on 26.9 and an error on 25.3 and 25.8
  (recorded: "a JSON number beyond 64 bits in a JSON column").
- `Point` is a JSON array of two numbers, `Ring`, `LineString` and
  `MultiPoint` are arrays of such points, and `Polygon`, `MultiLineString` and
  `MultiPolygon` nest one level more each (recorded: "a geo type" and "type
  MultiPoint"). The type name tells which one it is.
- An `AggregateFunction` is the state of the function as a JSON string of its
  bytes, with the escapes of a `String` (recorded: "an aggregate function
  state"). The state of `sumState` of a `UInt32` is 8 bytes, written as
  `"\u0005\u0000..."`. A `SimpleAggregateFunction` is a column of the type of
  its argument (recorded: "every type, JSONCompactEachRowWithNamesAndTypes").
- An `Interval` is a JSON number, and the unit is in the type name, such as `IntervalDay`. The recorded values came from expressions (recorded: "an interval").
- A `Time` and a `Time64(P)` are `"HH:MM:SS"` and `"HH:MM:SS.fff"`, signed,
  with the range -999:59:59 to 999:59:59. 25.3 has no such type. 25.8 refuses
  a column of that type until the setting `enable_time_time64_type=1` is set,
  and the error names that setting. 26.9 has it by default (recorded: "type
  Time and Time64, with no setting", "type Time and Time64 in a table, with no
  setting", "type Time and Time64 in a table, with its settings" and "type
  Time and Time64 select").
- A `QBit(Float32, N)` is a JSON array of N numbers on 26.9. 25.3 and 25.8
  refuse the type with the code 50 (recorded: "type QBit"). `MultiPoint` and
  `Geometry` are on 26.9 only. On 25.3 and 25.8 the name `Geometry` is a
  `String` (recorded: "type MultiPoint" and "type Geometry").
- `Nothing` is the type of `NULL` and of `[]` and `{}`: `Nullable(Nothing)`,
  `Array(Nothing)` and `Map(Nothing, Nothing)` (recorded: "the type Nothing
  and NULL").
- The `toTypeName` of a result column and the type name of the second line
  agree (recorded: "a type name from the server").

These facts come from the sources, and are not measured:

- clickhouse-go gives these Go types. The integers are `uint8` to `uint64` and `int8` to `int64`, and `*big.Int` for the 128 and 256 bit integers. A `Decimal` is the `decimal.Decimal` of `shopspring`, the dates and times are `time.Time`, and `Time` is `time.Duration`. An `Enum` and a `FixedString` are `string`, and `IPv4` and `IPv6` are `net.IP`. A `UUID` is the `uuid.UUID` of Google. An `Array` and a `Map` are typed slices and maps, and a `Nullable` is `*T`. `Variant`, `Dynamic` and `JSON` are its own `chcol` types (clickhouse-go,
  `lib/column`, `TYPES.md`). Its `Interval` scan type is a `string`
  (clickhouse-go, `lib/column/interval.go`).
- clickhouse-connect reads `AggregateFunction` as a type that it does not
  support, and it reads every other type of this table, `MultiPoint`, `QBit`,
  `Time` and `Time64` included (clickhouse-connect, `datatypes`).
- The server stores a `Time` as an `Int32` of seconds and a `Time64(P)` as an
  `Int64` of ticks. A `QBit` is a bit transposed vector (the documentation of
  ClickHouse, not measured).

### What JSON gives and what RowBinary gives

The first column is the type. The second says whether
`JSONCompactEachRowWithNamesAndTypes` is exact when a decoder keeps each
number as text and the settings are
`output_format_json_quote_64bit_integers=0`,
`output_format_json_quote_denormals=1` and `date_time_output_format=iso`. The
third says what `RowBinaryWithNamesAndTypes` gives. Each line rests on the
facts above and on the Python decoder.

| Type | JSON | RowBinary |
| --- | --- | --- |
| Integers up to 256 bits | exact | exact, little endian |
| `Float32`, `Float64`, `BFloat16` | exact, with the setting for NaN and infinity | the bits |
| `Decimal` up to 76 digits | exact, scale in the type name | scaled integer, scale in the type name |
| `Bool` | exact | one byte |
| `String`, `FixedString` | exact only with `AllowInvalidUTF8` and a read of the raw value | the bytes |
| `Date`, `Date32` | exact | days since the epoch |
| `DateTime`, `DateTime64` | exact only with `iso`, which writes UTC | ticks since the epoch |
| `Time`, `Time64` | exact | seconds and ticks |
| `Enum8`, `Enum16` | the name, the number is in the type name | the number |
| `UUID`, `IPv4`, `IPv6` | exact | 16 bytes of two little endian words, 4 bytes, 16 bytes |
| `Array`, `Tuple`, `Map` | exact, a named `Tuple` as an object, a `Map` key as text | exact |
| `Nullable`, `LowCardinality` | exact | one flag byte, or the inner type |
| `Variant`, `Dynamic`, `JSON` | the value, and not its member type | the member index, or the binary type name of each value |
| geo types | exact | `Float64` pairs and arrays |
| `AggregateFunction` | an opaque string of bytes | bytes with no length, function by function |
| `QBit` | exact | `Float32` elements |
| `Interval` | a number, the unit in the type name | a number |

## Parameters

These facts were recorded on each release:

- A typed parameter is `{name:Type}` in the statement and `param_name=value`
  in the query string, with GET or POST, or in a field of a multipart body
  (recorded: "typed parameters of each type", "typed parameters by GET" and "a
  multipart form with the query and a parameter"). The types of the recorded
  parameters were `Int32`, `String`, `Date`, `Array(Int32)`,
  `Nullable(String)`, `UInt64`, `Float64`, `Decimal(10, 2)`, `Bool`, `UUID`,
  `DateTime('Asia/Jakarta')`, `DateTime64(3, 'UTC')`, `Tuple(Int32, String)`,
  `Map(String, Int32)`, `Int128` and `IPv4`.
- The text of a parameter is the `TabSeparated` text of its type. A string
  with a tab, a newline or a backslash fails with the code 457, so each of
  them must be written as `\t`, `\n` and `\\` (recorded: "a parameter of a
  string with a tab, a newline and a backslash"). A NULL of a `Nullable` type
  is `\N`, and the text `NULL` fails with the code 457. An empty value of
  `Nullable(String)` is the empty string and not a NULL (recorded: "a
  parameter of NULL spelled NULL" and "a parameter of an empty string").
- A parameter that the statement does not use is ignored. A parameter that the
  statement needs and the request lacks is the code 456 and HTTP 500. A value
  that does not read as its type is the code 457 and HTTP 500 (recorded: "a
  parameter that is not used", "a parameter that is missing" and "a parameter
  of the wrong type").
- A value out of the range of its type wraps with no error: 300 for `Int8` is
  44 (recorded: "a parameter out of range"). A negative value for `UInt64` is
  the code 457 (measured with `curl` on `clickhouse-26.9` on 2026-10-07).
- A parameter has a type in the statement, and `{a}` with no type is a syntax
  error (recorded: "a parameter with no type").
- A parameter works in `LIMIT`, `WHERE`, an `IN` list as an `Array`, the
  values of an `INSERT`, `ALTER TABLE ... UPDATE` and `DELETE` (recorded: "a
  parameter in a limit", "a parameter in a where", "a parameter in an in
  list", "a parameter in an insert", "a parameter in an update" and "a
  parameter in a delete"). The same name twice is one value (recorded: "a
  parameter of the same name twice"). A name that repeats in the query string
  reads as its first value (recorded: "a parameter repeated in the query
  string"). A name of a setting is a name of a parameter too (recorded: "a
  parameter with a settings value of the same name").
- A parameter as `{t:Identifier}` is one quoted name, so `dbimp.dbimp_big` is
  a table of that name and not a database and a table (recorded: "a parameter
  as an identifier").
- A parameter in the definition of a view works (recorded: "a parameter in a
  view definition").
- The placeholders `?`, `$1`, `@a` and `:a` are not placeholders of the
  server: `?` and `@a` and `:a` are syntax errors, and `$1` is an identifier
  (recorded: "a question mark", "a dollar placeholder", "a named placeholder
  with an at sign" and "a colon placeholder"). A form of
  `application/x-www-form-urlencoded` does not carry a parameter (recorded: "a
  parameter in a query that is also in the body of a form").
- A string literal with `\'` and with `''` and with `\\` and `\t` reads as the
  text that it says, and a literal date is `DATE '2026-10-07'` (recorded: "a
  literal for a string with escapes" and "a literal date and time").
- The server converts the type of a value where it can. A `{p:String}`
  compared with a number worked on 26.9, as `1 = {p:String}` and `number =
  {p:String}` (measured with `curl` on `clickhouse-26.9` on 2026-10-07). A
  `DateTime64` parameter reads a decimal epoch such as `1793511000.123`, and a
  `Decimal(76, 40)` parameter keeps every digit. A parameter of `Float64` with
  the text `nan` gave `null` (the same measurement).

The server has no positional or named placeholder of the form that
`database/sql` uses. D34 gives the escaper of the root package for a product
with no binding. clickhouse-go binds `?`, `$1` and `@name` in its own code,
and with `clickhouse.Named` it sends a value as `{name:Type}` and `param_name`
(clickhouse-go, `bind.go` and `conn_http.go`, not measured).

## Transactions

These facts were recorded on each release:

- `BEGIN TRANSACTION` and `START TRANSACTION` answered HTTP 501 with the code
  48 `Transactions are not supported`, with and without a session (recorded:
  "begin a transaction", "begin a transaction in a session" and "start
  transaction").
- `COMMIT` and `ROLLBACK` in a session answered HTTP 500 with the code 649
  `There is no current transaction` (recorded: "commit a transaction in a
  session" and "rollback in a session").
- The setting `implicit_transaction=1` answered HTTP 501 with the code 48
  (recorded: "an implicit transaction"). `system.settings` has the settings
  `implicit_transaction` and `throw_on_unsupported_query_inside_transaction`
  and no setting that allows a transaction (recorded: "the setting that allows
  transactions").
- A statement that fails in the middle leaves its earlier writes. An `INSERT
  ... SELECT` of ten rows that failed on its fourth row left two rows
  (recorded: "an insert that fails in the middle" and "what the failed insert
  left").

These facts come from the sources, and are not measured:

- The documentation of ClickHouse says that transactions are experimental and
  need a server configuration with Keeper. The image of `dbrun` has none.
- clickhouse-go returns the connection as its transaction. `Commit` sends the
  batch that a `PrepareBatch` made, and `Rollback` closes the connection
  (clickhouse-go, `clickhouse_std.go`). clickhouse-connect has no
  transactions, and its `commit` and `rollback` do nothing
  (clickhouse-connect, `dbapi/connection.py`).

## Errors

These facts were recorded on each release:

- The status of an error that comes before the first byte of the body follows the code of the error. HTTP 400 is a syntax error (code 62) and a value that cannot be read (codes 6 and 27). HTTP 404 is an unknown table (60), database (81), column (47), function (46), format (73) and setting (115). HTTP 401 is a wrong password of the user `default` (194), and HTTP 403 is a wrong password of another user (516). HTTP 500 is a type error (43), a table that exists (57), a division by zero (153) and a write in readonly mode (164). HTTP 501 is a feature that is not there (48). HTTP 408 is a timeout (159), on 26.9 only (recorded: "an error before rows, a syntax error",
  "an error before rows, an unknown table", "an error before rows, an unknown
  database", "an error before rows, an unknown column", "an error before rows,
  an unknown function", "an error before rows, a type error", "an error before
  rows, a table that exists", "an error before rows, a division by zero in a
  constant", "an error before rows, an insert of a wrong value", "an error
  before rows, an unknown format", "an error before rows, an unknown setting",
  "a wrong password" and "max_execution_time").
- A user that has no privilege gets the code 497 as HTTP 500 on 25.3 and 25.8,
  and as HTTP 403 on 26.9 (recorded: "a read of a system table with no
  grant").
- The answer to an error before the first byte has the header
  `X-ClickHouse-Exception-Code` with the code, and 26.9 adds
  `X-ClickHouse-Exception-Tag` (recorded: "the headers of a response of a
  failed select"). The body is the text `Code: 60. DB::Exception: ...
  (UNKNOWN_TABLE) (version 26.9.2.8 (official build))`. On 25.3 and 25.8 the
  body of a `JSONCompactEachRowWithNamesAndTypes` answer is the line of names
  `[]`, the line of types `[]` and then the text in a JSON array of one string
  (recorded: "an error before rows, an unknown table"). On 26.9 it is the text
  alone. In the formats `JSONCompact`, `RowBinary` and `Native` the body is
  the text alone, except that `JSONCompact` on 25.3 and 25.8 has the key
  `exception` (recorded: "an error before rows, JSONCompact", "an error before
  rows, RowBinary" and "an error before rows, Native").
- An error after some rows has no fixed status. The same statement answered
  HTTP 200 on 25.3 and 25.8 and HTTP 500 on 26.9, and the status of one
  statement differed between two runs of the recorder, by timing (recorded:
  "an error after rows, throwIf in JSONCompactEachRowWithNamesAndTypes", which
  answered HTTP 200 for the administrator and HTTP 500 for the ordinary user
  on 25.8). The status is 500, with the rows before the error in the body,
  when the server had not yet sent the headers.
- With HTTP 200 on 25.3 and 25.8, the formats of JSON end with a row that is a
  JSON array of one string, the text of the error, such as `["Code: 395.
  DB::Exception: ..."]`, and the formats `TabSeparated`, `RowBinary` and
  `Native` end with the marker `__exception__\r\n` and the text (recorded: "an
  error after rows, throwIf in JSONCompactEachRowWithNamesAndTypes", "an error
  after rows, in JSONCompactEachRow", "an error after rows, in
  RowBinaryWithNamesAndTypes", "an error after rows, in Native" and "an error
  after rows, in TabSeparated"). A decoder cannot tell that last JSON row from
  a row of a table with one string column.
- With the setting `http_write_exception_in_output_format=0`, 25.3 and 25.8
  write the marker also for the JSON formats (recorded: "an error after rows,
  with no exception in the format"). The setting is 1 by default on 25.3 and
  25.8 and 0 on 26.9 (recorded: "the settings of the quotes").
- On 26.9 an error with HTTP 200 is the marker, a random tag, the text, the
  length of the text, the tag and the marker, each on its own line after a
  line break, and the tag is in the header `X-ClickHouse-Exception-Tag` of the
  answer (recorded: "an error after rows, in Native"). An answer with HTTP 500
  holds the rows that the server wrote first and then the text, with no marker
  (recorded: "an error after rows, in TabSeparated").
- An error that comes from the data in a row, such as a number that cannot be
  read, is HTTP 200 and an error row on 25.3 and 25.8, and HTTP 400 with the
  rows before it on 26.9 (recorded: "an error after rows, a string that is not
  a number").
- The error in a statement of `INSERT` before the first byte is an HTTP 4xx or
  5xx (recorded: "an error before rows, an insert of a wrong value").
- `max_execution_time` ended a query with the code 159. The answer was an
  error row with HTTP 200 on 25.3 and 25.8 and HTTP 408 on 26.9 (recorded:
  "max_execution_time").
- A query that is too deep is the code 167 (`TOO_DEEP_AST`), and the limit is
  1000 (recorded: "a very long statement"). A query longer than
  `max_query_size` is the code 62 (recorded: "a query that is too large").
- Two queries with one `query_id` at one time: the second answered HTTP 500
  with the code 216 (recorded: "a second query with the query id in use").
- No answer was HTTP 429 or HTTP 503. A user with
  `max_concurrent_queries_for_user` at 1 got HTTP 500 with the code 202 `Too
  many simultaneous queries` for its second query (measured with `curl` on
  `clickhouse-25.8` on 2026-10-07). A quota with `MAX queries = 2` did not
  stop a third query in the same test, so the answer to a quota is not
  measured.
- Every error that the server writes shows that the request reached it. Only a
  failure to connect, before any byte of the request left, is an error of a
  request that did not reach the server.

## Cancellation and timeouts

These facts were recorded on each release (the first line of each pair is the
request that the client abandoned after 1 second, and the second is a later
request of the same principal that asked `system.processes`):

- When the client leaves while rows flow, the server can notice at its next
  write. On 25.3 and 25.8 the query was gone 3 seconds after the client left,
  and on 26.9 it was still in `system.processes` after 3 seconds. The query
  was `sleepEachRow(0.2)` over 30 rows, with `max_block_size=1` (recorded: "a
  client that gives up while rows flow" and "the query after the client gave
  up while rows flow"). The reason on 26.9 is not measured.
- When the client leaves and no row flows, the query kept running to its end.
  It was in `system.processes` 0.5 seconds and 3 seconds after the client
  left, and gone after 8 seconds, at its own end (recorded: "a client that
  gives up while no row flows", "the query after the client gave up while no
  row flows", "the query three seconds after the client gave up while no row
  flows" and "the query a few seconds after the client gave up while no row
  flows").
- The setting `cancel_http_readonly_queries_on_client_close=1` with
  `readonly=1`, or with GET, stopped the query: it was gone 3 seconds after
  the client left. The setting alone with POST did not stop it (recorded: "a
  client that gives up with the readonly cancel while no row flows", "the
  query after the client gave up with the readonly cancel", "a client that
  gives up by GET with the cancel setting while no row flows", "the query
  after the client gave up by GET", "a client that gives up with the cancel
  setting and no readonly while no row flows" and "the query after the client
  gave up with the cancel setting and no readonly"). The setting is 0 by
  default on each release (recorded: "the settings of the quotes").
- `KILL QUERY WHERE query_id = '...' SYNC` stopped a query that ran in the
  background. It answered one row with `kill_status` `finished`, and the
  killed request ended with the code 394 `Query was cancelled` (recorded: "a
  query that runs in the background", "the running query", "KILL QUERY by
  query_id" and "the query after the kill"). A kill of an id that is not
  running answered an empty result (recorded: "KILL QUERY of a query that does
  not exist").
- The ordinary user killed its own query, and a `KILL QUERY` for the queries
  of the user `default` answered an empty result for it (recorded: "KILL QUERY
  by query_id", for the ordinary user, and "KILL QUERY of a query of another
  user").
- A `query_id` of a query that is running makes the second query fail with the
  code 216. With `replace_running_query=1` the second query ran and the first
  ended with the code 394 (recorded: "a second query with the query id in
  use", "a query that replaces the running query with the same id", "the query
  that replaces it" and "the first query after it was replaced").
- `max_execution_time` ends a query by itself (see Errors). The setting
  `http_send_timeout` and `http_receive_timeout` changed nothing in a small
  query (recorded: "the send timeout and the receive timeout").
- A session is held on the server by `session_id`. A temporary table and a
  `SET` stayed in the session, and a request with another session or none did
  not see them (recorded: "a session: select from the temporary table", "a
  session: select with no session", "a session: the setting after" and "a
  session: the setting in another request"). A second request on a session
  that runs a query got the code 373 `Session is locked by a concurrent
  client` (recorded: "a session: the same session at the same time"). A
  session that does not exist with `session_check=1` got the code 372
  (recorded: "a session: the session check"). `close_session=1` ended the
  session (recorded: "a session: close it" and "a session: the temporary table
  after the close"). A `USE` in a session did not change the database of the
  next request, since the query key `database` of that request chose it
  (recorded: "a session: the use of a database" and "a session: the database
  after use").
- The server has `Keep-Alive: timeout=10` or 30 (see Responses), so it closes
  an idle connection after that time. Not measured: the behavior of a client
  that sends on a connection at the moment of the close.

These facts come from the sources, and are not measured:

- clickhouse-go passes the deadline of the context as the setting
  `max_execution_time` (the wire report, for its native protocol) and sets
  `ResponseHeaderTimeout` of its transport to `ReadTimeout`, which ended a
  long query whose headers come late (clickhouse-go issue 1924).

## Statements

These facts were recorded on each release:

- A request holds one statement. Two statements are HTTP 400 with the code 62
  `Multi-statements are not allowed` (recorded: "two statements in one
  request"). The driver does not need to split a statement, and the server
  refuses a split that the caller leaves in.
- An `INSERT ... VALUES` followed by `; SELECT 1` was HTTP 200 with an empty
  body. The server wrote the row of the `INSERT` and did not run the `SELECT`
  (recorded: "two statements, an insert and a select" and "the rows that the
  inserts wrote").
- A `;` at the end is accepted, alone or with a newline, a line comment or a
  block comment after it (recorded: "a trailing semicolon", "a trailing
  semicolon and a newline", "a trailing semicolon and a comment" and "a
  trailing semicolon and a block comment").
- A comment is `-- text` to the end of the line, `# text` to the end of the
  line, and `/* text */`, and a block comment can nest. A comment can hold a
  `;`. A statement that is only a comment is HTTP 400 `Empty query` (recorded:
  "a line comment", "a block comment", "a hash comment", "a nested block
  comment", "a comment only" and "a comment with a semicolon"). A string with
  `;`, `--` and `/*` is a string (recorded: "a string with a semicolon").
- Leading white space and newlines are accepted (recorded: "a statement with a
  leading space and a newline").
- A `SETTINGS` clause and a `FORMAT` clause after it are accepted (recorded:
  "a statement with a settings clause" and "a statement with a format after a
  settings clause").
- An `INSERT` takes its data after the statement, in the same body, with
  `FORMAT JSONEachRow`, `CSV`, `TabSeparated` or `JSONCompactEachRow`. It also
  takes its data as the whole body, with the statement in the query key
  `query` (recorded: "an insert with data after the statement", "crud: insert
  as JSONEachRow", "crud: insert as CSV", "crud: insert as TabSeparated",
  "crud: insert as JSONCompactEachRow" and "an insert with the data in the
  body and the statement in the query").
- `DESCRIBE`, `EXPLAIN`, `EXISTS TABLE`, `WITH`, `USE` and `SET` answered HTTP
  200 (recorded: "a describe", "an explain", "a statement with exists", "a
  with clause", "a statement with the keyword use" and "a statement with the
  keyword set"). A `USE` or a `SET` without a session changes nothing for the
  next request.
- A statement that nests 1000 levels is HTTP 400 with the code 167 (recorded:
  "a very long statement"). The `max_query_size` default is not measured, and
  a value of 5 refused a statement (recorded: "a query that is too large").
- The statements `INSERT ... ON DUPLICATE KEY UPDATE`, `REPLACE INTO`, `INSERT
  ... RETURNING`, `MERGE INTO` and `SELECT ... FOR UPDATE` are syntax errors
  (recorded: "crud: upsert", "crud: replace into", "crud: insert returning",
  "crud: merge" and "crud: select for update"). `INTO OUTFILE` is refused with
  the code 358 (recorded: "crud: select into outfile").
- `DELETE FROM t` with no `WHERE` is a syntax error. `TRUNCATE TABLE` empties
  a table (recorded: "crud: delete with no where" and "crud: truncate").
- The `SELECT *` of a table leaves out the `MATERIALIZED` and `ALIAS` columns
  (recorded: "schema: a default value select star").
- A primary key does not make the rows unique. Two rows with one key were both
  stored, and a `ReplacingMergeTree` keeps one row for each key after a merge,
  or at a read with `FINAL` (recorded: "schema: a duplicate primary key",
  "schema: the rows of the duplicate primary key", "schema: a replacing table
  select with final" and "schema: a replacing table select without final"). A
  `FOREIGN KEY` clause in `CREATE TABLE` was accepted and dropped: the stored
  table has no such clause, and a row with a parent that does not exist was
  stored (recorded: "schema: a foreign key in a table", "schema: the foreign
  key as the server stored it", "schema: a row that breaks the foreign key"
  and "schema: the row that broke the foreign key"). A `CHECK` constraint is
  enforced with the code 469 (recorded: "schema: a check constraint is
  enforced").
- `ALTER TABLE ... UPDATE` and `ALTER TABLE ... DELETE` are mutations that run
  in the background, and `mutations_sync=2` waits for them (recorded: "crud:
  update with a mutation", "crud: delete with a mutation" and "crud: the
  mutations of the table"). The lightweight `DELETE FROM ... WHERE` is a form
  of mutation that hides the rows at once (recorded: "crud: delete, the
  lightweight form" and "crud: select after the delete").
- The changes to the columns of the key are refused with the code 524
  (recorded: "schema: alter modify column" and "schema: alter rename column").

## Principals

- The administrator is `default`. The image sets its password from
  `CLICKHOUSE_PASSWORD`, which is the test value of `container.Password`, and
  `CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1` lets it create users and grant (the
  dbmeta entry).
- `dbrun dsn --json` names one principal for each release, the administrator
  (measured on 2026-10-07). There is no ordinary user. The setup of
  `requests.json` makes `dbimp_user` as the administrator, with `CREATE USER`
  and a password, and grants `SELECT, INSERT, ALTER, CREATE, DROP, TRUNCATE,
  OPTIMIZE` on the database `dbimp` and `SELECT` on `system.processes`
  (recorded: "setup: create the ordinary user", "setup: grant the ordinary
  user the database dbimp" and "setup: grant the ordinary user its own
  processes"). The wire report made one ordinary user for each type of
  password in the same way, with its own code.
- On 25.8 and 26.9 the ordinary user got the status of the administrator for
  each request of the recording, except for the requests that these lines
  name. It ran every statement of CRUD and of the schema in its own database
  (recorded: the requests of item 3, for both principals).
- The ordinary user cannot read most of `system` and `information_schema`:
  `system.users`, `system.build_options`, `system.server_settings`,
  `system.data_skipping_indices`, `system.dictionaries`, `system.parts`,
  `information_schema.columns` and `information_schema.schemata` gave the code
  497. `system.settings` and `system.processes` (granted) worked (recorded: "a
  read of a system table with no grant", "the version from the build", "the
  version, the full text", "schema: the indexes of a table", "schema: the
  dictionary in the system table", "schema: the partitions of the table",
  "schema: the information schema" and "the settings of the quotes as the
  ordinary user"). The ordinary user cannot create a database or a user, flush
  logs, `CHECK` a table, `dictGet` with no grant, use `remote` or `url`, or
  make a table with the engine `Kafka` (recorded: "a create of a database with
  no grant", "a create of a user with no grant", "feature: the system flush",
  "feature: the check table", "schema: a dictionary lookup", "feature: the
  remote table function", "feature: the url table function" and "schema: the
  Kafka engine").
- The ordinary user sees what it has a grant for. `SHOW DATABASES` gave
  `dbimp` and `system`, and `SHOW TABLES FROM dbimp` gave the tables of
  `dbimp`. A table of a database with no grant is as if it did not exist: the
  read, the write and the drop answered HTTP 404 with the code 60 (recorded:
  "show the databases", "show the tables", "a read of a database with no
  grant", "a write to a database with no grant" and "a drop with no grant").
- The version statement works for both principals on each release. `SELECT
  version()` gave `25.3.14.14`, `25.8.33.6` and `26.9.2.8` (recorded: "the
  version"). `usql` sets no `Version` statement, and the model of `dbmeta`
  runs `SELECT version()` (usql and the model). `SELECT value FROM
  system.build_options` is refused to the ordinary user (recorded: "the
  version from the build").
- A wrong password of `default` is HTTP 401 with the code 194, and a wrong
  password of another user is HTTP 403 with the code 516, with the header
  `WWW-Authenticate: Basic realm="ClickHouse server HTTP API"` on the 401
  (recorded: "a wrong password" and "the headers with an unauthorized
  request"). A user that does not exist is HTTP 403 with the code 516
  (recorded: "a user that does not exist by the query string").
- The setting `readonly=1` refuses a write with the code 164 and HTTP 500.
  `readonly=2` accepted a read (recorded: "the setting readonly set by the
  client" and "the setting readonly set to 2").

## Flavors

No other product is behind this interface. The driver serves ClickHouse only.
The releases differ, and these are the differences that the recording shows:

| Fact | 25.3 | 25.8 | 26.9 |
| --- | --- | --- | --- |
| `output_format_json_quote_64bit_integers` by default | 1 | 0 | 0 |
| `http_write_exception_in_output_format` by default | 1 | 1 | 0 |
| `enable_http_compression` by default | not measured | not measured | 1 |
| Header `X-ClickHouse-Exception-Tag` | no | no | yes, on every answer |
| `Keep-Alive` timeout | 10 | 10 | 30 |
| An error after rows | row or marker, HTTP 200 | row or marker, HTTP 200 | text or marker with a tag, HTTP 500 or 200 |
| Timeout of a query | error row, HTTP 200 | error row, HTTP 200 | HTTP 408 |
| The code 497 | HTTP 500 | HTTP 500 | HTTP 403 |
| `Time` and `Time64` | none | with `enable_time_time64_type` | by default |
| `QBit`, `MultiPoint`, `Geometry` | none | none | yes |
| `Nullable(Tuple)` and `Nullable(Point)` | no | no | yes |
| `UPDATE ... SET` on a table with `enable_block_number_column` | syntax error | yes | yes |
| `LIVE VIEW` | with a setting | with a setting | syntax error |
| `Float32` max text and `toFloat64('5e-324')` | 3.4028233e38 and 0 | 3.4028233e38 and 0 | 3.4028235e38 and 5e-324 |
| A query that writes rows, after the client left | gone | gone | still running after 3 seconds |

The sources are in the sections above: the settings in "the settings of the
quotes" (recorded), the tag and the `Keep-Alive` in "the headers of a response
of a select" (recorded), and the rest in the lines that name them. The default
of `enable_http_compression` on 25.3 and 25.8 is not a recorded value: the
recorded answers show no compression there (see Responses).

The server tells its release in the statement `SELECT version()`, in a text
such as `25.8.33.6` (recorded: "the version"). No header of the response holds
the version. `X-ClickHouse-Exception-Tag` is a sign of the framing of 26.9 and
later (recorded: "the headers of a response of a select").

## Interfaces

`clickhouse/tables_test.go` writes this table from the code (step 10).

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares, and each connection runs SELECT version() once (D177). |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the user and the password. GET /ping needs neither. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because the driver sends no session, and the settings go with each request. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option, and the values that the driver binds with a type of their own: a uint64, a decimal, a big integer, the types of the root package, a UUID, an address, a list and a map (D176). |
| `driver.QueryerContext` | yes | The driver binds each argument as a typed parameter of the server, {pN:Type} in the statement and param_pN in the query string (D176). |
| `driver.ExecerContext` | yes | Exec reads the result to its end, and RowsAffected fails, because the server counts no row that a statement changed (D176). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because the server answers every BEGIN with HTTP 501 (D177). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, so an answer has one result. |
| `driver.RowsColumnTypeScanType` | yes | The second line of the answer names the type of each column, with its arguments, and each type has one Go type (D135 and D177). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The type of a column in upper case, without its arguments and its wrappers, such as INT8 or DATETIME64. |
| `driver.RowsColumnTypeLength` | yes | A FixedString has its length in its type. |
| `driver.RowsColumnTypeNullable` | yes | A column can be NULL when its type is Nullable or holds a NULL of its own, and the type table says which types can. |
| `driver.RowsColumnTypePrecisionScale` | yes | A Decimal has its precision and its scale in its type. |
<!-- /dbimp:interfaces -->

The facts that the table needs are in the sections above. `Ping` runs
`SELECT 1`, which checks the user and the password. `GET /ping` answers `Ok.`
and costs the server no query, but it takes no user and no password, so it does
not show a wrong one (recorded: "the ping endpoint"). A connection holds no
state on the server, so the driver has no `SessionResetter`. The driver sends no
`session_id`, so a temporary table and a `SET` do not stay between two
statements (see Open questions).

## Faults

These are the faults of the driver that `usql` uses now, which this driver
must not repeat. Each one comes from the source, or from an issue that was
read on 2026-10-07. None was measured against a server here.

- `RowsAffected` of `sql.Result` returns 0 for every statement (clickhouse-go
  issue 1139). `usql` sets its own `RowsAffected` that returns 0 (usql).
- `BeginTx` returns a transaction that is the connection itself. `Commit`
  sends the batch that `PrepareBatch` made, and `Rollback` closes the
  connection (clickhouse-go, `clickhouse_std.go`). The server has no
  transaction (see Transactions), and D20 says that the driver returns
  `ErrNotSupported`.
- `ResponseHeaderTimeout` of the transport is `ReadTimeout`, which ended a
  long query whose headers come late, while the server went on to finish it
  (clickhouse-go issue 1924). `dbimp.NewTransport` sets no such timeout.
- The deadline of the context goes to the server as the setting
  `max_execution_time`, and a user who cannot change that setting gets an
  error (clickhouse-go issue 1107, and the wire report).
- The connection goes back to the pool after a decode error with its read
  stream in the middle of a response (clickhouse-go issue 1988). D36 says that
  a connection is closed in that case.
- A decode error while the driver reads the block of an exception, and an EOF
  on the first request, were reported for the HTTP mode (clickhouse-go issue
  1916).
- `PrepareBatch` removes `FORMAT` and `VALUES` from the statement with code
  that does not read quotes, so a column list or a quoted value can be dropped
  or corrupted (clickhouse-go issue 2012). It sends the `query_id` of the
  caller with its internal `DESCRIBE TABLE` too, so two queries can have one
  id (clickhouse-go issue 2033). This driver has no batch.
- `CheckNamedValue` accepts every value and returns no error (clickhouse-go,
  `clickhouse_std.go`). W4 says that a driver returns `driver.ErrSkip` for a
  value that it does not take.
- The Go type of a value of the scan depends on the type of the column, with
  its own types for `Variant`, `Dynamic` and `JSON`, and a `string` for an
  `Interval` (clickhouse-go, `lib/column`). D135 gives one Go type for each
  kind.
- A memory leak in the encoding of a column over HTTP was reported
  (clickhouse-go issue 1637).
- The HTTP mode sends the user and the password in the headers
  `X-ClickHouse-User` and `X-ClickHouse-Key` (clickhouse-go, `conn_http.go`).
  That works on each release when no `Authorization` header is sent (see
  Requests).

The driver here repeats none of them. A test holds each one:

- `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, and never 0
  (`TestReplayNoColumns`).
- `BeginTx` returns `dbimp.ErrNotSupported` (`TestContract`).
- The transport sets no `ResponseHeaderTimeout`, and the deadline of the
  context never becomes a setting. Only `WithTimeout` sends
  `max_execution_time`, and only when the caller asks for it. A context that
  ends sends `KILL QUERY` for the query (`TestKillsWhenTheContextEnds` and
  `TestIntegrationCancel`).
- Rows that the caller closes before the end close the body, so no connection
  goes back to the pool with half a response, and the driver sends `KILL QUERY`
  (`TestKillsWhenTheRowsCloseEarly`).
- The marker of an error after some rows is read from the bytes that the
  decoder holds and from the body, in both forms, and a marker that is cut short
  is an error that wraps `dbimp.ErrIncomplete` (`TestStreamEnds` and
  `TestRecordedTrailer`).
- The driver has no batch. An `INSERT` is a statement that the caller writes,
  and the driver never changes its text (`TestIntegrationCRUD`).
- `CheckNamedValue` returns `driver.ErrSkip` for a value that it does not take
  (`TestCheckNamedValue`).
- Each kind has one Go type, so an `Interval` is a `dbimp.Interval`, and a
  `Variant`, a `Dynamic` and a `JSON` are the decoded JSON value
  (`TestScanTypesMatchDecode`).
- The driver reads one row at a time, and holds no more than a row (`TestLargeResult`).
- The driver sends the user and the password in the `Authorization` header only
  (`TestRequest`).

## Second opinions

Gemini (`gemini-3.1-pro-preview`) and DeepSeek (`deepseek-v4-pro`) were asked
on 2026-10-07. DeepSeek timed out on most of its calls, and the calls with a
short prompt and at most 8000 tokens of answer got through. Gemini timed out
on the first long prompt and then answered.

Step 5a. Both models answered the four questions of the survey, in separate
conversations. `features.json` holds each entry and the model that named it.

- Gemini said: `INSERT`, `SELECT`, and mutations `ALTER TABLE ... UPDATE` and
  `DELETE`, that are asynchronous, and a lightweight `DELETE FROM`. The server
  agrees. Gemini said that the primary key is a sort key and not a constraint,
  that there is no foreign key, and that `UNIQUE` is not enforced. The server
  agrees: a duplicate key was stored, and a `FOREIGN KEY` clause was accepted
  and dropped. It said that `ReplacingMergeTree` and `CollapsingMergeTree`
  deduplicate later, and that `FINAL` and `SAMPLE` and `PREWHERE` exist. The
  server agrees (recorded: "schema: a replacing table select with final",
  "schema: a collapsing table select with final", "feature: SAMPLE with a
  sampling key" and "feature: PREWHERE"). It said that `Live` and `Window`
  views exist. The server answered an error for `LIVE VIEW` and `WINDOW VIEW`
  on 26.9, and `LIVE VIEW` worked with a setting on 25.3 and 25.8 (recorded:
  "schema: a live view with its setting" and "schema: a window view with its
  setting").
- Gemini said that `output_format_json_quote_64bit_integers=1` is the default.
  It is the default on 25.3 and not on 25.8 and 26.9 (recorded: "the settings
  of the quotes"). It said that `Decimal` loses digits unless
  `output_format_json_quote_decimals=1`. The server wrote every digit with the
  default (recorded: "every type, JSONCompactEachRowWithNamesAndTypes"). It
  said that a `DateTime` has no loss in JSON. The server shows that a time in
  the hour of the autumn change is not unique in the text (see Responses).
- Gemini said that a mutation is asynchronous, and that `X-ClickHouse-Key` and
  `X-ClickHouse-User` pass the credentials in headers. The server agrees.
- DeepSeek said: `INSERT` in single, batch and async forms, `SELECT`, `ALTER
  TABLE ... UPDATE|DELETE`, a lightweight `DELETE`, a lightweight `UPDATE` "in
  modern ClickHouse", and no `UPSERT` or `MERGE`. The server agrees with each
  statement. The lightweight `UPDATE` needs a table that has the column
  `_block_number` (recorded: "crud: lightweight update on a table with the
  block number column").
- DeepSeek said: no foreign key, no unique constraint, and skipping indexes,
  materialized views, dictionaries, projections, `ReplacingMergeTree` and
  `CollapsingMergeTree`, the codecs, the partitions, and the table engines and
  functions `S3`, `Kafka` and `URL`. The server agrees with each one
  (recorded: "schema: a projection", "schema: a dictionary", "schema: a column
  with a codec", "schema: drop a partition", "schema: the Kafka engine",
  "feature: the url table function" and "feature: the s3 table function"). The
  function `url` answered a refusal to connect, and `s3` answered a refusal of
  the credentials of the server on 26.9.
- DeepSeek gave the ranges of the integers and `Float32` and `Float64`, and
  its answer to the rest of the types did not arrive. Gemini gave the types of
  the survey.

Step 7. Each lead was sent to the server.

- Gemini: "Use `X-ClickHouse-User` and `X-ClickHouse-Key`". They work alone,
  and with an `Authorization` header they are HTTP 403 (see Requests).
- Gemini: "Support `multipart/form-data` for external tables". It works with
  `ext_structure` and `ext_format` in the query string (recorded: "a multipart
  form with an external table").
- Gemini: "Process `X-ClickHouse-Summary`" and "`X-ClickHouse-Progress`". Both
  exist (recorded: "a response with the summary of a select" and "a result
  with the progress in headers").
- Gemini: "Validate `100 Continue`". The header `Expect: 100-continue` changed
  nothing in the answer (recorded: "a request with the header Expect").
- Gemini: "Handle HTTP 307 redirects when query routing is enabled in a
  cluster". No answer of the single node was a redirect. Not measured for a
  cluster, which `dbrun` does not start.
- Gemini: "Query `/replicas_status`". It answered `Ok.` on one node (recorded:
  "the replicas status endpoint").
- Gemini: "Switch long queries from GET to POST". GET makes the request
  readonly (recorded: "a GET that writes"). A GET with a URL of 60000 bytes
  answered HTTP 404 (measured with `curl` on `clickhouse-25.8` on 2026-10-07).
- Gemini: "`wait_end_of_query=1` ensures that the response blocks until the
  operations are done". It holds the whole result and gives the real status
  (recorded: "an error after rows, with wait_end_of_query").
- Gemini: "Use the `X-ClickHouse-Format` header". It works (recorded: "a
  request with the header X-ClickHouse-Format").
- Gemini: "Parse `X-ClickHouse-Timezone`". It exists and follows
  `session_timezone` (recorded: "the header X-ClickHouse-Format and the
  timezone").
- Gemini: "`readonly` gives HTTP 403". It gave HTTP 500 with the code 164
  (recorded: "the setting readonly set by the client").
- Gemini: "`X-ClickHouse-Quota`". The header was accepted with no effect that
  the test showed (recorded: "a request with the header
  X-ClickHouse-Quota").
- Gemini: "`X-ClickHouse-Default-Roles`". It was accepted and ignored, on 25.3
  (measured with `curl` on `clickhouse-25.3` on 2026-10-07).
- Gemini: "`Arrow` or `Parquet` for bulk reads". Both worked (recorded:
  "columns, Arrow" and "feature: format Parquet").
- Gemini: "TLS on port 8443". Not measured, because the image has no
  certificate.
- Gemini: "Apply `query_id` for idempotency and safe retries". A second query
  with a running `query_id` fails, so a retry of a statement with the same id
  fails while the first runs (recorded: "a second query with the query id in
  use"). That is a way to refuse a double run, not a replay of the answer.
- DeepSeek: "Pass settings as URL parameters". They work (recorded: "feature:
  the settings in the query string").
- DeepSeek: "Always set a `query_id` for `KILL QUERY`". It works (recorded:
  "KILL QUERY by query_id").
- DeepSeek: "Treat HTTP 200 as partial success, since errors can come in the
  body". They do (see Errors).
- DeepSeek: "Handle `Content-Encoding` in responses even if you did not ask
  for compression". On 25.3 and 25.8 the answer is not compressed unless the
  request asks, and on 26.9 it is compressed by default for a client that
  sends `Accept-Encoding` (see Responses).
- DeepSeek: "Encode parameters as `param_<name>`". They work (see Parameters).
- DeepSeek: "The default format is `TabSeparated`". It is (recorded: "a
  statement with no default format").
- DeepSeek: "A multi-statement request is fine with a POST body". It is not:
  HTTP 400 (recorded: "two statements in one request").
- DeepSeek: "Do not implement transactions". The server has none (see
  Transactions).
- Both models named sessions and temporary tables. They work (recorded: "a
  session: select from the temporary table").

Step 8a. Both models reviewed the mapping, from the list of step 1 and the
rules of D135, in separate conversations.

- Gemini agreed with every row except two. It said that `String` and
  `FixedString` are the kind binary and `[]byte`, because they can hold bytes
  that are not UTF-8. It said that `DateTime` and `DateTime64` are the kind
  local timestamp, because the JSON has no offset and a time in the hour of
  the autumn change is ambiguous, and that no mapping works for a `DateTime64`
  with a zone. The server shows that a time with `date_time_output_format=iso`
  is the instant in UTC (recorded: "a date time with each output format,
  iso"), and that `RowBinary` gives the instant (measured with the Python
  decoder), so the instant is available and the zone is in the type name. The
  table keeps the kind timestamp and `time.Time`, which needs that setting.
  Gemini said that a `Tuple` stays the kind tuple and `[]any`, and that the
  driver must set the format of a named tuple as an array, or read the object
  in the order of its fields. The server writes a named tuple as an object
  unless `output_format_json_named_tuples_as_objects=0` (recorded: "a tuple
  with names and with no names").
- DeepSeek agreed with the integers, `Decimal`, the floats, the strings,
  `Date`, `Time` as a duration, the arrays, tuples, maps, json and
  `AggregateFunction` as bytes. It said that `DateTime` is a `time.Time` only
  with `date_time_output_format='iso'`. It said that a NaN and an infinity
  arrive as `null`, so `float64` loses them unless
  `output_format_json_quote_denormals=1`. The server agrees (recorded: "NaN
  and infinities by default" and "NaN and infinities, quoted"). It said that
  an `Interval` needs the unit of the type name. The server shows that the
  value is a bare number and the unit is only in the type name (recorded: "an
  interval"). It said that the wrappers `Nullable`, `LowCardinality`,
  `SimpleAggregateFunction` and `Nested` are unwrapped and have no row. The
  table has no row for them. It said that `map[string]any` writes the key of
  another type as text, and that a driver that needs the exact key type parses
  the type name. The server shows that such a key is its text (recorded: "a
  map with each key type"). It said that a geometry as nested `[]any` of
  `float64` loses the type, and that a typed form or WKT is better. The server
  writes only the arrays of numbers, and the type name tells which type it is.
  See Open questions.

## Open questions

Each is a fact that the recording shows and a choice that the recording cannot
make. D176 and D177 closed questions 1 to 11, and Ken decided question 15 on
2026-10-07. Questions 12 to 14 and 16 to 23 still wait for Ken.

1. Closed in `dbmeta` v0.3.0, which Ken committed, and the workflow of CI pins
   it. The entry of ClickHouse in `dbmeta` publishes the HTTP port 8123 as the
   second port, prints the `api` address of each release, and makes the
   ordinary user `dbmeta_user`, who has `SELECT, INSERT, ALTER, CREATE DATABASE,
   CREATE TABLE, CREATE VIEW, CREATE DICTIONARY, DROP DATABASE, DROP TABLE, DROP
   VIEW, DROP DICTIONARY, TRUNCATE, OPTIMIZE` on the database `dbmeta` and
   `SELECT` on `system.processes` (measured: `SHOW GRANTS` on 2026-10-07). The
   tests of step 14 ran on a checkout that held the change before the release,
   with no change to it. The tests read `CLICKHOUSE_SECOND_ADDRESS`, which the
   workflow sets from `secondAddress`, to reach the HTTP port.
2. Closed by D176. DRIVER.md says to ask Ken when a server refuses one of insert, select,
   update and delete. ClickHouse refuses `UPDATE ... SET` on a table that has
   no `_block_number` column, and on every table on 25.3, and it has `ALTER
   TABLE ... UPDATE`, which works on each release but runs in the background
   unless `mutations_sync=2` is set. Does the driver pass `UPDATE` through as
   the caller wrote it, and document `ALTER TABLE ... UPDATE`? The
   `RowsAffected` of any statement is not known, because
   `X-ClickHouse-Summary` holds 0 for an update and a delete (see Requests).
3. Closed by D176. The format (step 9, items 4 and 12). The proposal is
   `JSONCompactEachRowWithNamesAndTypes` with these settings on every request:
   `output_format_json_quote_64bit_integers=0`,
   `output_format_json_quote_denormals=1`, `date_time_output_format=iso`,
   `output_format_json_named_tuples_as_objects=0` and
   `http_write_exception_in_output_format=0`, read with `jsontext` that allows
   invalid UTF-8 and keeps each number as text. It is exact for each type
   except the member type of a `Variant`, the type of a `Dynamic` and of the
   paths of a `JSON`, the zone of an instant, and an `AggregateFunction`. The
   other choice is `RowBinaryWithNamesAndTypes`, which is exact for every type
   except `AggregateFunction` (with no length in the format, and
   `clickhouse-connect` does not read it either), and which needs Ken's
   approval (D13) and a decoder of about 1,200 lines. See Responses and Types.
4. Closed by D177. The Go types of step 8a (item 4 of step 9) are these.
   `Int128`, `Int256`, `UInt128` and `UInt256` are the kind decimal.
   `FixedString` is the kind string. `IPv4` and `IPv6` are the kind other.
   `Time` and `Time64` are the kind duration. The geometries are nested
   `[]any`. `AggregateFunction` is the kind binary. `DateTime` and `DateTime64`
   are the kind timestamp. Each has an alternative in Types.
5. Closed by D176. The parameters (item 6). The server binds `{name:Type}` with a type, and
   has no `?`. The driver can write each argument as a typed parameter, with
   the type chosen from the Go type of the argument, or write each argument as
   a literal with the escaper of D34. The server converts a `String` parameter
   in a comparison with a number, so the driver can send every value as a `String`. That is not measured for an insert.
6. Closed by D176. How the driver reads the end of a result (item 8). The status and the
   framing of an error differ by release and by the moment of the error (see
   Errors). With `wait_end_of_query=1` the status is reliable and the server
   holds the result. Without it, the driver must treat any status other than
   200 as an error, even when rows came first, and for HTTP 200 it must read
   the marker or the row of the error by release.
7. Closed by D176. The cancel (item 9). A disconnect stops a query at its next write on 25.3
   and 25.8, and on 26.9 not at the next write. `KILL QUERY` with a `query_id`
   stops it for sure. The setting
   `cancel_http_readonly_queries_on_client_close=1` with `readonly=1` stopped
   a query that wrote nothing, but it makes the request readonly.
8. Closed by D177. The DSN (item 3). `dburl` writes `clickhouse://` for the native protocol
   and `http://` or `https://` for the HTTP transports, and the driver here
   must read a URL whose scheme is `clickhouse` (D35). The names clash with
   `clickhouse-go`, which also registers `clickhouse`, so `usql` cannot link
   both. `dburl` D34 waits for this driver. D177 item 2 decides the keys, the
   port 8123 and the TLS: the one key `tls`, and the port 8443 with
   `tls=true`.
9. Closed by D177. Transactions (item 7): `BeginTx` returns `ErrNotSupported` (D20), as the
   server refuses every form with HTTP 501.
10. Closed by D177. Redirects and credentials (item 10): no redirect was seen, and the
    credentials go in the Basic header of each request. `X-ClickHouse-User`
    and `X-ClickHouse-Key` are the other form, and the server refuses a
    request that sends both forms.
11. Closed by D177. The flavors (item 11): there is one product. The releases differ (see
    Flavors), and the driver can tell them from `SELECT version()` and from
    the header `X-ClickHouse-Exception-Tag`.
12. The recorder cannot send a body that is not text, so a request with a gzip
    body or a `RowBinary` insert is not recorded. It sends the header
    `Authorization` always, so the request with `X-ClickHouse-User` alone is
    only measured with `curl`. A change of the recorder would let it send
    both. The shared code is not changed here.
13. `dbmeta` D167 says that `dbrun` gives each product an `api` address. The
    `api` of ClickHouse needs the user `default` and the password of
    `container.Password`. The model of `dbmeta` reads the native port.
14. Not measured: the quota header, a quota that stops a query, TLS, the
    behavior of a cluster, a request through a proxy, the keep-alive close
    race of an idle connection, `X-ClickHouse-SSL-Certificate-Auth`, and JWT.
15. 26.9 compresses the answer by default for a client that accepts gzip, and
    the compressor holds the rows of a query that produces them slowly. A query
    of `sleepEachRow(0.2)` over 300 rows gave its first row to the driver after
    about 60 seconds, when the query ended, on 26.9, and gave a row every 0.2
    seconds on 25.3 and 25.8, and on 26.9 with `enable_http_compression=0`
    (measured by the integration tests on 2026-10-07). The transport of
    `dbimp.NewTransport` accepts gzip, as DRIVER.md says. Ken decided on
    2026-10-07 that the driver sends `enable_http_compression=0` on every
    request, as a sixth setting after the five of D176. A caller can still turn
    it on with `WithParameter`. This costs the bandwidth of a large result.
16. A bound argument has a Go type, and some types have none. A `Tuple` has no Go
    type that the driver can bind, because `[]any` is an `Array` and a slice of
    values of two types is an error, so a caller writes `JSONExtract(?, 'Tuple(...)')`
    with JSON text, as the round trip does. A `Time` and a `Time64` are
    `time.Duration` when read, but `database/sql` turns a `time.Duration` into an
    `int64` of nanoseconds before the driver sees it, so a caller binds the text
    `12:34:56`, as the round trip does. The driver can take a `time.Duration` in
    `CheckNamedValue` and bind it as `Time64(9)`, which changes what a caller who
    binds a `time.Duration` for an integer column gets.
17. 26.9 refuses a `DateTime64` parameter in `ALTER TABLE ... UPDATE` for an
    instant whose number of seconds has fewer than ten digits, such as the epoch
    and anything before 2001, with the code 41. It stores the parameter in the
    mutation as the string `'0'` and reads it back as the text of a time
    (measured with `curl` on `clickhouse-26.9` on 2026-10-07). A `String`
    parameter with `toDateTime64` and an `Int64` parameter with
    `fromUnixTimestamp64Nano` update the same instant. The driver binds a
    `time.Time` as `DateTime64(9, 'UTC')`, as D176 says, so a caller meets the
    refusal. The round trip skips those updates on 26.9.
18. `database/sql` closes the rows of `QueryRow` after the first row, with no
    call of `Next` after it, so the driver cannot tell that the result had ended
    and sends `KILL QUERY` for the query, as D176 says for rows closed before
    the end. A `QueryRow` is then two requests, and the second one finds no
    query. The driver can skip the cancel when it knows that the body is at its
    end, which it cannot know without a read that can block.
19. `WithReadonly(true)` sends `readonly=1`. The server then refuses a write and
    a change of a setting in the text of the statement, and accepts the settings
    in the query string, which are the settings of the driver (measured on all
    three releases by `TestIntegrationFeatures`). D109 asks for an option
    that says "write nothing", and `readonly=2` is the other choice, which
    accepts a `SETTINGS` clause.
20. A caller who writes the typed placeholder `{name:Type}` of ClickHouse in a
    statement and passes `sql.Named("name", v)` gets `dbimp.ErrArguments`,
    because the driver finds only `?` and `@name`. D176 says that the driver
    writes the placeholders. The driver can read `{name:Type}` too and send
    `param_name` with the text of the argument, which D176 does not say.
21. `INSERT ... VALUES ({p1:Type})` can fail for a column of the type `Dynamic`,
    because the parser of `VALUES` reads the braces as a map before it reads a
    parameter (measured on 25.8: the column held the text `{p:Int64}`). `INSERT
    ... SELECT ?, ?` works for every type, and the round trip uses it. The
    driver does not change the text of a statement.
22. The transport lets go of an idle connection after 5 seconds. The server closes
    an idle connection after 10 seconds on 25.3 and 25.8, and after 30 on 26.9
    (the header `Keep-Alive`), and the driver sends a `POST` once, so a request on
    a connection that the server has just closed is an error. DRIVER.md says
    that the connector builds its transport with `dbimp.NewTransport`, which
    sets 90 seconds, and the connector changes that one field. The race itself
    is not measured.
23. Hard rule 4 of `AGENTS.md` lists the drivers that store a context. This driver
    stores none: the rows and the watch of a query hold the function `ctx.Err` and a
    closure, and the cancel runs with `context.WithoutCancel`. So `AGENTS.md`
    needs no change for it.

## Integration tests

The integration tests of the driver read `CLICKHOUSE_DSN`, and
`CLICKHOUSE_ORDINARY_DSN` for the ordinary user, and skip when `CLICKHOUSE_DSN`
is empty. `CLICKHOUSE_SECOND_ADDRESS` replaces the host and the port of each
DSN, because the `url` of `dbrun` holds the native port. The tests accept the
`api` address of `dbrun` as well. They ran on 2026-10-07, one server at a time,
with `-race`, on a fresh container of each release, against a checkout of
`dbmeta` that held the change of question 1 before its release:

| Release | Passed | Skipped | Failed |
| --- | --- | --- | --- |
| `clickhouse-25.3` | 412 | 11 | 0 |
| `clickhouse-25.8` | 415 | 10 | 0 |
| `clickhouse-26.9` | 415 | 10 | 0 |

A count is a test or a subtest, and each principal is a subtest. Each skip names
the release that its entry is about, such as `TestIntegrationCRUD/25.3
lightweight update`, which runs on 25.3 only. Every test that needs the tables of
a run runs as the administrator, in a database of its own, and as the ordinary
user `dbmeta_user`, in the database `dbmeta` that it can write, with the name of
the run as the prefix of each table. `TestMain` drops the database of the run,
looks for a table that a test left in either database, drops it, and fails.

The round trip stores every type that `features.json` marks yes in a column of
that type, with the values of DRIVER.md, as a bound argument and as a literal,
and updates, reads and deletes each one (`TestIntegrationRoundTrip`). These types
cannot go in a column in the plain way, and the round trip says how it stores
each one:

- `Nothing` cannot be the type of a column (the code 370). The table holds a
  `Nullable(Int8)`, and the select makes a `Nullable(Nothing)` from the stored NULL
  with `if(isNull(v), NULL, NULL)`, the one expression that has that type.
- A `Tuple`, a geometry and a `QBit` have no Go type to bind, so the argument is
  the JSON text of the value, and the statement reads it with `JSONExtract`.
- A `Time` and a `Time64` take their text as the argument, because a
  `time.Duration` becomes an `int64` (see Open questions).
- An `Interval` is stored as the integer of the number of days, with
  `toIntervalDay(?)`, in a column of the type `IntervalDay`.
- A `Variant` has the members `Int64`, `String` and `Array(Int64)`, because the
  server converts a value to a `Variant` only when its type is a member.
- An `AggregateFunction` takes the integer that its state is made from, with
  `sumState(toUInt32(?))`, and an update that sets it uses a subquery.

These facts came from the integration tests. The recordings did not show them,
and each is measured on the release that it names, on 2026-10-07:

- The server closes the connection after it wrote the marker and the text of an
  error after some rows, with no last chunk, on every release. The body then ends
  with `io.ErrUnexpectedEOF` after the text, and the recorded exchanges have the
  flag `truncated`. The driver takes the text and ignores that end
  (`TestIntegrationErrorAfterRows`). A statement that writes about 20 MiB before
  it fails gives the error after its rows with HTTP 200 on all three releases.
- A `Time` and a `Time64` end with a `Z` on 25.8 when the driver sets
  `date_time_output_format=iso`, such as `12:34:56Z`. The driver drops it. The
  tests pass on 26.9 with or without it.
- `readonly=1` refuses a write, and a `SETTINGS` clause in the text, and it
  accepts the settings in the query string, such as `max_threads`, with the key
  of `readonly` before or after them.
- `ALTER TABLE ... UPDATE ... SETTINGS mutations_sync = 2` works, and so does the
  setting in the query string. A parameter in a mutation works, and a parameter of
  `Int64` out of the range of an `Int8` column wraps (300 became 44 on 25.8).
- 25.3 refuses to update a column of the type `Dynamic` or `JSON` (the code 420).
  A `JSON` column on 25.3 gives the number `1` for a number of the object and the
  strings `"1.5"` for the numbers inside an array.
- 26.9 refuses a `DateTime64` parameter in a mutation for an early instant (Open
  questions).
- On 25.3 the name `Time` is an alias of `Int64`, and `Time64` is an unknown type
  (the code 50). On 25.8 the function `toTime` of a string is the old function
  that takes a `DateTime`, so `CAST('12:34:56' AS Time)` writes the value.
- A bound `Float64` of the largest `Float32` reads back as that value on every
  release, where `toFloat32` of its text reads one step below on 25.3 and 25.8. A
  bound `Float64` of `5e-324` reads as 0 on 25.3 and 25.8, as `toFloat64` of its
  text does.
- A mutation refuses an aggregate function (the code 184), and it takes the same
  function in a scalar subquery.
- `KILL QUERY ... SYNC` answers a row of `kill_status`, `query_id`, `user` and
  `query`, and the killed query ends with the code 394.
- The ordinary user cannot use `dictGet`, the Kafka engine, `url` or `s3` (the
  code 497), and cannot create a database. On 26.9 the administrator gets the code
  497 from `s3` too, because the server refuses to use its own credentials for a
  user query.
- The block of `Native` with `compress=1` starts with a checksum of 16 bytes and
  the method, which is LZ4 (`0x82`) or ZSTD (`0x90`), and the test accepts both.

## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-07 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of ClickHouse from the sections above.

### The server

| | Couchbase | ClickHouse |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /` with the SQL text as the body, and the settings and the parameters in the query string (Requests) |
| Database | The key `query_context` of the body | The query key `database`, or the header `X-ClickHouse-Database` (Requests) |
| Language | SQL++, which is close to SQL | The SQL of ClickHouse, one statement for each request. A `;` at the end is accepted (Statements) |
| DDL | In SQL++ | In SQL. A table has an engine, a primary key is a sort key and not a constraint, and there is no foreign key (Statements) |
| Parameters | `?`, `$1` and `$name` | `{name:Type}` in the statement and `param_name` in the query string, each with a type (Parameters) |
| Framing | One body for the whole result, which does not page | One body that streams, with a line for each row, and no paging (Responses) |
| Columns | `signature`, before the first row | The first two lines of the body, the names and the exact type names, before the first row. A repeated name stays (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement (Responses) |
| Errors | Can come with HTTP 200, after some rows | A status that follows the code, before the first byte. After some rows, the marker `__exception__` with HTTP 200, or the text with HTTP 500 on 26.9, and the connection closes (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with the type name of each column. With five settings every value is exact, except the member type of a `Variant` and the paths of a `JSON` (Types) |
| Cancel | The server stops a query when the client leaves | A query can run on when the client leaves, and `KILL QUERY` with the `query_id` stops it (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` answers HTTP 501 (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic, or the headers `X-ClickHouse-User` and `X-ClickHouse-Key`, or the keys `user` and `password`. The server refuses two forms in one request (Requests) |
| Default port | 8093, or 18093 with TLS | 8123, or 8443 with TLS |

The differences that a caller sees:

- A statement has no result count, and `RowsAffected` always gives an error
  (D176).
- The driver binds an argument as a typed parameter, and chooses the type from the
  Go type of the argument. A `Tuple`, a `time.Duration` for a `Time` and a named
  argument that matches `{name:Type}` have no form (D176 and Open questions).
- An error after some rows reaches the caller after the rows, from the marker,
  and wraps `dbimp.ErrIncomplete`. A status other than 200 is an error even when
  rows came first (D176).
- The server does not always stop a query when the client leaves, so the driver
  sends `KILL QUERY` when the context ends and when the rows close early, and a
  `QueryRow` sends it too (D176 and Open questions).
- `UPDATE ... SET` goes to the server as the caller wrote it, and the server
  refuses it on a table with no block number column. `ALTER TABLE ... UPDATE` works
  on every release and runs in the background unless `mutations_sync` is 2 (D176).
- There are no transactions, and `BeginTx` returns `dbimp.ErrNotSupported`
  (D177).

### The driver

| | `couchbase` | `clickhouse` |
| --- | --- | --- |
| Size, without tests, on 2026-10-07 | About 1300 lines in 8 files | About 2700 lines in 10 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `Database`. The DSN has the one key `tls` (D177) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter` and `WithDatabase`, through `WithOptions` or an argument (D109). `WithTimeout` sets `max_execution_time` in seconds with a fraction. `WithReadonly(true)` sets `readonly=1`. `WithParameter` sets a setting of the server, and a name that the driver sets itself takes the value of the caller. `WithDatabase` sets the key `database` |
| Arguments | Sent to the server as `args` and `$name` | Written as typed parameters, `{pN:Type}` and `param_pN`, from the Go type of each argument. `?` and `@name` are placeholders. A struct, a list of values of two types, a decimal with no value and an integer of more than 256 bits are refused (`clickhouse/params.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | A reader of its own for the format, which reads the two lines of the header and then one row for each call, and reads the marker of an error from what the decoder holds and from the body (`clickhouse/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | The same, and `ColumnTypeLength` for a `FixedString` and `ColumnTypePrecisionScale` for a `Decimal`, from the type name. `ColumnTypeNullable` is true for a `Nullable` and for the types that hold a NULL of their own |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `uint64`, `*big.Int` for 128 and 256 bits, `netip.Addr`, `time.Time` for an instant in the zone of the column, `time.Duration` for a `Time`, the types of the root package, and nested `[]any` for a geometry (D135 and D177) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` always give `dbimp.ErrNotSupported` (D176) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D177) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | Each query has a `query_id`. When the context ends before the driver read the whole answer, and when the caller closes the rows early, the driver sends `KILL QUERY ... SYNC` with a limit of 5 seconds (D176) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Code, Name, Message}`, which unwraps to `*dbimp.StatusError` when the error came with a status |
| Authentication | Basic | Basic only, and the driver follows no redirect, so the credentials go to the host of the DSN only (D177) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a time, a decimal, a UUID, an address and an interval
  too, where Couchbase gives JSON shapes (D135 and D177).
- An integer of 128 or 256 bits is a `*big.Int`, and an address is a
  `netip.Addr` (D177).
- A `Variant`, a `Dynamic` and a `JSON` give the decoded JSON value, with no member
  type (D176).
- `RowsAffected` always gives an error, where Couchbase counts every statement
  with `mutationCount` (D176).
- A connection runs `SELECT version()` when it opens, so an open sends one request
  (D177).
- `WithTimeout` sets `max_execution_time`, and the DSN has no key for it, where
  Couchbase has `Timeout` in its configuration (D176).
- A statement with a `FORMAT` clause is refused with `dbimp.ErrNotSupported`,
  because the driver reads one format (D176).
