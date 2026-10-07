# OpenSearch

This file holds what is known about OpenSearch, for the driver `opensearch`
(W29 and D162). The headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a and 6 measured 2.19.6 on 2026-10-01 and 2026-10-02, and 3.9.0 on
2026-10-07, as
`admin`, the administrator, and as `dbmeta_user`, an ordinary user who can
read the indices whose names start with `dbmeta`. A fact marked "recorded"
is in `testdata/opensearch/`, and the name in quotes after it is the name of
its request in `requests.json` there. The two releases gave the same
answers, except where a line says otherwise. The recordings of 3.8.0 were
replaced by those of 3.9.0 (D178, item 7). A fact that names 3.9.0 was recorded
or measured on 3.9.0. A fact marked "measured" names
how it was measured. Each section starts with the measured facts. The facts
after them come from the sources of step 3, and each one that is not
measured says so. The sources, each read on 2026-10-01, are these:

- "The dbmeta entry" is `container/opensearch.go` in `dbmeta`. It starts
  the image `docker.io/opensearchproject/opensearch` with the security
  plugin on and TLS off on the HTTP port 9200, so a password works over
  plain HTTP. It writes the hash of the password of `admin` into
  `internal_users.yml` before the first start, because `admin` is a
  reserved user that the REST interface cannot change. Its `Init` makes the
  role `dbmeta_role`, which has `read`, `indices:admin/mappings/get` and
  `indices:monitor/settings/get` on the indices `dbmeta*`, the user
  `dbmeta_user` with that role, and the index `dbmeta`. dbmeta D118 says
  that the SQL plugin needs `indices:monitor/settings/get` for a user that
  reads.
- "The Go client" is `github.com/opensearch-project/opensearch-go` v5.0.0,
  at its commit `51b50d1` of 2026-10-01: `plugins/sql` and the type
  `SQLQuery` of `opensearchapi`.
- "The JDBC driver" is `github.com/opensearch-project/sql-jdbc`, the Java
  driver of the project, at its commit `e87a393` of 2026-08-07: the types in
  `OpenSearchType.java` and the request in `JsonQueryRequest.java`.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-10-01 and 2026-10-02.
- "Elasticsearch" is [ELASTICSEARCH.md](ELASTICSEARCH.md), which another
  session measured on the same days. OpenSearch forked from Elasticsearch
  7.10, and each line that starts with "Unlike Elasticsearch" names a
  difference between the two.

## Summary

- OpenSearch is a search engine with a document store. A query reaches it
  in the Query DSL, in PPL, or in SQL through the SQL plugin at `POST
  /_plugins/_sql`. The driver reads SQL only. The Query DSL fails S
  ([TARGETS.md](TARGETS.md)), and PPL is a note under Flavors.
- R holds. `dbrun` starts `opensearch-2.19.6` and `opensearch-3.9.0`, each
  in the Staged tier with the cadence `tested` (the dbmeta entry, and
  measured with `dbrun list --json all` on 2026-10-01 for 3.8.0, which
  `dbrun` no longer starts, and 2026-10-07 for 3.9.0). Each one started,
  and `GET /` gave its version (recorded: "the version").
- H and S hold. `POST /_plugins/_sql` answered SQL with HTTP 200 as both
  principals on each release (recorded: "a statement").
- SQL in OpenSearch reads only. `INSERT`, `UPDATE`, `CREATE TABLE`, `DROP
  TABLE`, `CREATE FUNCTION` and `BEGIN` fail with HTTP 400, `Query must
  start with SELECT, DELETE, SHOW or DESCRIBE` (recorded: "an insert", "an
  update", "a create table", "a drop table", "a create function" and
  "begin"). `DELETE` fails with HTTP 400, `DELETE clause is disabled by
  default and will be deprecated`, because the setting
  `plugins.sql.delete.enabled` is false (recorded: "a delete"). A write goes
  through the document API, such as `POST /<index>/_bulk` (recorded: "fill
  the index of every type").
- The priority is P1 ([TARGETS.md](TARGETS.md)). Ken gave Elasticsearch and
  OpenSearch a driver each (D162).
- `dburl` has no scheme for OpenSearch (measured with `grep` in
  `dburl/scheme.go` on 2026-10-01). `usql` has no driver for it (measured
  with `ls usql/drivers` on 2026-10-02).
- `dbmeta` has no model for OpenSearch. Its entry exists only so that
  `dbrun` can start a server for this driver (the dbmeta entry, dbmeta
  D118).
- The driver is `github.com/xo/dbimp/opensearch` (W29 and D168). Steps 10 to
  17a went in on 2026-10-07. `dbrun` now starts 2.19.6 and 3.9.0 and no longer
  starts 3.8.0. Step 6 was recorded again on 3.9.0 on 2026-10-07 (Open
  questions, 17), and the integration tests ran on 2.19.6 and 3.9.0
  (Integration tests).

## Requests

These facts were recorded on each release:

- A statement is `POST /_plugins/_sql` with a JSON object whose `query`
  holds the text (recorded: "a statement"). With no `format`, the answer is
  the `jdbc` format (recorded: "a statement" and "a statement in the jdbc
  format"). A path that ends with a slash works too (recorded: "a path with
  a slash at the end").
- `GET` fails with HTTP 405 (recorded: "a GET of the endpoint"). A
  `Content-Type` of `text/plain` fails with HTTP 406 (recorded: "a statement
  as plain text"). A body that is not JSON fails with HTTP 400, `Failed to
  parse request payload` (recorded: "a body that is not JSON").
- The body takes `query`, `fetch_size`, `cursor`, `parameters` and `filter`
  with the Query DSL (recorded: "a statement with a filter of the query DSL"
  and the requests of items 4 and 5). A key that the server does not know,
  such as `"unknown": 1`, does not fail as such. It sends the statement to
  the legacy engine, which then failed on `SELECT 1` with a
  `NullPointerException` (recorded: "a statement with a key that the server
  does not know"). `wait_for_completion_timeout` does the same (recorded: "a
  statement with a time to wait").
- `format` in the query string takes `jdbc`, `csv` and `raw` on each
  release, and `json` on 2.19.6 only. On 3.9.0, `json` fails with HTTP 400,
  `unknown response format: json`. `tsv` and `yaml` fail on each release
  (recorded: "a statement in the json format", "a statement in the tsv
  format" and "a format that does not exist").
- `POST /_plugins/_sql/_explain` gives the plan of a statement, with the
  request that it sends to the search engine, and does not run it
  (recorded: "explain a statement").
- `POST /_opendistro/_sql`, the path of Open Distro, works on 2.19.6, and
  fails with HTTP 400, `no handler found`, on 3.9.0 (recorded: "the old path
  of Open Distro").
- Authentication is HTTP basic. A wrong password fails with HTTP 401, a body
  of plain text `Unauthorized`, and `WWW-Authenticate: Basic
  realm="OpenSearch Security"` (recorded: "a wrong password").
- 3.9.0 sends the header `X-OpenSearch-Version: OpenSearch/3.9.0
  (opensearch)` with every answer, an error and an answer to the ordinary
  user too. 2.19.6 sends no such header (recorded: "a statement" and "a
  wrong password").
- Unlike Elasticsearch, the path is `/_plugins/_sql` and not `/_sql`, the
  rows are `datarows` and the columns `schema`, a parameter has a type, and
  the body takes no `time_zone`, `columnar`, `params` or `request_timeout`.

These facts come from the sources, and are not measured:

- The Go client sends `format=jdbc` by default, and can send `sanitize`,
  which turns off the guard of the `csv` format against formulas (the Go
  client). With `sanitize=false`, the value `-2147483648` lost the quote
  that the guard puts before it (recorded: "a statement in the csv format
  with no sanitizing").
- Amazon OpenSearch Service and OpenSearch Serverless can require AWS
  Signature Version 4 (Gemini).

## The DSN

- `dburl` has no scheme, so step 9 decides the URL (D27 and D35).
- The dbmeta entry gives each principal as `http://user:password@host:port`,
  with the port 9200 inside the container. `dbrun` publishes it on a port of
  its own, 55132 for 2.19.6 and 55133 for 3.9.0 (measured with `dbrun dsn
  --json` on 2026-10-02).
- The JDBC driver has properties for the type of a key store and a trust
  store, for the compression of a request, and for AWS Signature Version 4
  (the names of its files under `src/main/java/org/opensearch/jdbc`, not
  measured).
- D168 decides the URL: `opensearch://user:password@host:9200`, with no path,
  the keys `tls` (false by default) and `fetch_size` (1000 by default), and
  every other key refused. The port is 9200 with and without `tls=true`. The
  `url` that `dbrun` prints for each principal already has the scheme
  `opensearch` (measured with `dbrun dsn --json` on 2026-10-07), so the tests
  read it as it is.

## Responses

These facts were recorded on each release:

- An answer of the `jdbc` format is one JSON object: `schema`, an array of
  `name`, `type` and sometimes `alias`, then `datarows`, an array of arrays in
  the order of `schema`, then `total`, `size` and `status` (recorded: "a
  statement"). So the columns arrive before the first row, and rule 1 of
  D18 applies. The server writes the JSON with indents and new lines.
- A column with `AS` has its label in `alias` and its expression in `name`,
  such as `{"name": "n", "alias": "x"}`. A column with no alias has only
  `name`, which is the text of the expression, such as `n + 1` (recorded:
  "a column with an alias").
- The columns keep the order of the statement (recorded: "columns in the
  order of the statement"). `SELECT *` gives the fields in an order that is
  neither the order of the mapping nor the order of the names, the same on
  both releases (recorded: "every type" and "every column with a star").
  Unlike Elasticsearch, which sorts them by name.
- Two columns with one name fail with HTTP 400, `Multiple entries with same
  key: n=1 and n=1` (recorded: "two columns with one name"). Two parameters
  of one value fail the same way (recorded: "two parameters of one value").
  Unlike Elasticsearch, which allows two columns with one name.
- A result with no rows has `schema` and an empty `datarows` (recorded: "no
  rows").
- `_id`, `_index` and `_score` are columns that a statement can name
  (recorded: "the metadata fields"). Unlike Elasticsearch.
- `csv` and `raw` are text with `Content-Type: plain/text`. `csv` writes an
  object as `{a=1, b=x}`, an array as `[1, 2, 3]`, and the number
  -2147483648 as `'-2147483648` (recorded: "a statement in the csv
  format"). So `csv` loses the type of each value.
- `json` on 2.19.6 is the answer of the search engine, with `hits` and each
  document in `_source` (recorded: "a statement in the json format").

Paging, as recorded on each release:

- `fetch_size` turns on a cursor. With 100, the first answer has `schema`,
  100 rows and `cursor`, a string that starts with `n:` (recorded: "a cursor
  of 100 rows"). `POST /_plugins/_sql` with `{"cursor": ...}` gives the next
  page, with `schema` again, the rows and a new `cursor` (recorded: "the
  second page" and "the third page"). The page after the last row has no
  rows and no `cursor` (recorded: "the page after the last row"). `total`
  and `size` are the rows of the page.
- Each `cursor` serves once. A page that was read already fails with HTTP
  404, `SearchContextMissingException` (recorded: "a page that was read
  already"). Unlike Elasticsearch, where a cursor can be read twice.
- `POST /_plugins/_sql/close` with a cursor answers `{"succeeded": true}`,
  and the same the second time. A closed cursor then fails with HTTP 404. A
  cursor that is not one fails with HTTP 500, `Unsupported cursor`
  (recorded: "close the cursor", "close the cursor again", "a page of a
  closed cursor" and "close a cursor that is not valid"). Unlike
  Elasticsearch, which answers `false` the second time.
- A cursor is a point in time of the search engine, with a `keep_alive` of
  60000 milliseconds. `GET /_search/point_in_time/_all` lists it (recorded:
  "the open cursors"). The setting `plugins.sql.cursor.keep_alive` is `1m`.
  A value of `2s` did not end a cursor that was read 8 seconds later
  (recorded: "keep a cursor for 2 seconds" and "a page after the time of the
  cursor ran out").
- `fetch_size` of 0 turns paging off (recorded: "a fetch size of 0").
  `fetch_size` of 20000 fails with HTTP 400, `Result window is too large`,
  because `index.max_result_window` is 10000 (recorded: "a fetch size above
  the window").
- A statement with `LIMIT` and `fetch_size` goes to the legacy engine. Its
  cursor starts with `d:`, its first page has `schema` and `total` of the
  whole result, 250, and its next page has only `cursor` and `datarows`
  (recorded: "a cursor with a limit" and "the second page of a cursor with a
  limit").
- A `GROUP BY` with `fetch_size` goes to the legacy engine, which gives 200
  groups, no cursor, and the type `double` for a `keyword` column (recorded:
  "a group by with a fetch size").

The limits of a result, as recorded:

- `plugins.query.size_limit` is 10000 on each release. A statement with no
  `LIMIT` and no `fetch_size` gives at most that many rows. With the limit
  set to 100, 300 rows gave 100 rows. On 2.19.6 the answer has no cursor and
  no sign that rows are missing. On 3.9.0 it has a `cursor`, and the page of
  that cursor has no rows (recorded: "300 rows with a size limit of 100", and
  on 3.9.0 "300 rows with a size limit of 100, and its cursor" and "the page
  after a size limit of 100").
- `LIMIT 250` with the limit at 100 gave 250 rows on 2.19.6, and 100 rows,
  with no sign, on 3.9.0 (recorded: "a limit of 250 with a size limit of
  100").
- On 2.19.6 a `GROUP BY` or a `DISTINCT` gives at most 1000 groups, with no
  cursor and no sign. 1050 groups gave 1000, `LIMIT 1050` gave 1000, and
  `LIMIT 50 OFFSET 1000` gave none (recorded: "a group by of 1050 groups", "a
  group by of 1050 groups with a limit", "a group by with an offset past
  1000" and "a distinct of 1050 values"). The plan shows a composite
  aggregation of size 1000 (recorded: "explain a statement").
- On 3.9.0 the same statements gave all 1050 groups, and the offset worked.
  `plugins.query.buckets` is 10000 there, and set to 100 it cut 300 groups
  to 100 with no sign (recorded: the same requests, and "300 groups with a
  limit on groups of 100").
- Neither `plugins.sql.query.bucket.size` nor `plugins.sql.group_by.limit`
  is a setting (recorded: "a setting for the size of a group by" and "a
  setting for the limit of a group by").
- 3.9.0 runs the Calcite engine: `plugins.calcite.enabled` is true there,
  and 2.19.6 has no such setting (recorded on 3.9.0: "the settings of the
  SQL plugin", and measured with `curl` of `/_cluster/settings` on 2.19.6 on
  2026-10-01).
- 3.9.0 has the setting `plugins.sql.complex_worker_pool.enabled`, true, which
  3.8.0 lacked (recorded on 3.9.0: "the settings of the SQL plugin").
- On 3.9.0 the index `.plugins-ml-config` exists, so `SELECT * FROM
  .plugins-ml-config` gives HTTP 200 with five columns and no rows, where
  3.8.0 gave HTTP 404 (measured on 3.8.0 and not measured again), and
  `SHOW TABLES LIKE %` lists it. The row of a `long` field that the source
  holds as 1.9 reads as NULL on 3.9.0, where 2.19.6 reads 1 (recorded:
  "every type") and 3.8.0 read 1 (measured on 3.8.0 and not measured again). The driver decodes both
  answers, so it needs no change.
- A request with `Accept-Encoding: gzip` gets `Content-Encoding: gzip`
  (recorded: "a gzip answer"). No answer was a redirect, and no answer was
  HTTP 429.

These facts come from the sources, and are not measured:

- The legacy engine runs a statement that the new engine cannot parse, and
  each release keeps it (Gemini and DeepSeek). The recordings above show
  which statements reached it.
- `opendistro.query.size_limit` of 200 is the limit of the legacy engine
  (measured with `curl` of `/_cluster/settings` on 2.19.6 on 2026-10-01).

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table
on 2026-10-02 from the measurements below, and Ken reviewed it in D168. The
code writes it now (step 10), and `TestTables` fails when this table is not
the one of the code.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| undefined | null | `nil` | `interface {}` | `UNDEFINED` | yes |
| boolean | boolean | `bool` | `bool` | `BOOLEAN` | yes |
| byte | integer | `int64` | `int64` | `BYTE` | yes |
| short | integer | `int64` | `int64` | `SHORT` | yes |
| integer | integer | `int64` | `int64` | `INTEGER` | yes |
| long | integer | `int64` | `int64` | `LONG` | yes |
| float | float | `float64` | `float64` | `FLOAT` | yes |
| double | float | `float64` | `float64` | `DOUBLE` | yes |
| half_float | float | `float64, from the legacy engine` | `float64` | `HALF_FLOAT` | yes |
| scaled_float | float | `float64, from the legacy engine` | `float64` | `SCALED_FLOAT` | yes |
| keyword | string | `string` | `string` | `KEYWORD` | yes |
| text | string | `string` | `string` | `TEXT` | yes |
| ip | string | `string, because the server gives the text and no type of Go fits both forms` | `string` | `IP` | yes |
| binary | binary | `[]byte, from base64` | `[]uint8` | `BINARY` | yes |
| date | date | `dbimp.Date, from the text of a day` | `dbimp.Date` | `DATE` | yes |
| time | time of day | `dbimp.LocalTime` | `dbimp.LocalTime` | `TIME` | yes |
| timestamp | timestamp | `time.Time, in UTC, because the server writes each instant in UTC with no zone` | `time.Time` | `TIMESTAMP` | yes |
| datetime | timestamp | `time.Time, in UTC, as a timestamp of 3.9.0` | `time.Time` | `DATETIME` | yes |
| geo_point | geometry | `map[string]any, with lat and lon, as the server writes it` | `map[string]interface {}` | `GEO_POINT` | yes |
| object | map | `map[string]any` | `map[string]interface {}` | `OBJECT` | yes |
| nested | array | `[]any, of a map[string]any for each nested object` | `[]interface {}` | `NESTED` | yes |
| integer_range | range | `map[string]any, with gte and lte, because the server sends an object and the driver never returns the text of JSON` | `map[string]interface {}` | `INTEGER_RANGE` | yes |
<!-- /dbimp:types -->

The wire type is the `type` of a column in `schema`. The database type is
the wire type in upper case. A value that is a JSON array in a column of a
type that is not `nested`, which is a field with several values (below), is a
`[]any` of the Go values of its type, as D168 decides, and `NULL` inside the
array is nil. A column whose type the driver does not know, such as `struct`
in the legacy engine, gives the decoded JSON value, with the scan type `any`.
A value that does not fit its type, such as a number in a `keyword` column,
fails the row with `dbimp.ErrInvalidValue`, and the driver never gives it as
text (hard rule 3, and Open questions, 15).

These facts were recorded on each release, from the index `dbmeta_types`,
which has one field of each mapping type and five documents (recorded:
"every type"):

- A NULL is JSON `null`. A field set to `null`, and a field that a document
  does not have, both arrive as `null`. So NULL and a missing value are one
  value. The literal `NULL` has the type `undefined` (recorded: "the
  literals of each type").
- `byte`, `short`, `integer` and `long` are JSON numbers, and a `long` keeps
  every digit from -9223372036854775808 to 9223372036854775807. A literal
  of 9223372036854775808 fails with HTTP 400, `NumberFormatException`
  (recorded: "an integer beyond the range of a long").
- A `float` keeps 3.4028235E38 and 1.4E-45, and a `double` 0.1. A
  `half_float` field arrives as `float`, and 65504 and 0.1 keep their text.
  A `scaled_float` field with the factor 100 arrives as `double`, and
  12.345 arrives as 12.345, from the source of the document and not rounded
  by the factor. Unlike Elasticsearch, which gives 1234.57 for 1234.5678
  with the same factor.
- There is no decimal type. The literal
  `123456789012345678901234567890.5` is a `double`,
  1.2345678901234568e+29 (recorded: "a number with many digits"). `1/0`
  and `SQRT(-1)` give `null` (recorded: "a division by zero and an
  overflow").
- A `keyword` and a `text` are strings, and keep `é'"\ x` and the empty
  string. A `binary` is a string in base64, such as `"AP8="`, and the empty
  string for an empty value.
- An `ip` is a string, such as `"192.168.0.1"` and `"::1"`. `CAST(ip AS
  STRING)` fails with HTTP 400, because the cast takes no `IP` (recorded: "a
  geo point and an ip in a function").
- A `date` field arrives as `timestamp`, as text in UTC with no zone:
  `2026-10-01T12:34:56.123+05:30` arrives as `"2026-10-01 07:04:56.123"`,
  and the number 1790858096123 as `"2026-10-01 12:34:56.123"`. The year
  -1000 arrives as `"1001-01-01 00:00:00"`, so the era is lost. A
  `date_nanos` field arrives as `timestamp` with nine digits, such as
  `"2262-04-11 23:47:16.854775807"`.
- A `date` field with the format `yyyy-MM-dd` arrives as `date`, such as
  `"2026-10-01"` and `"0001-01-01"`. A `date` field with the format
  `HH:mm:ss` arrives as `time`, such as `"12:34:56"`.
- The literals `DATE '0001-01-01'` to `DATE '9999-12-31'`, `TIME
  '23:59:59.999999999'` and `TIMESTAMP '9999-12-31 23:59:59.999999999'`
  keep every digit (recorded: "the bounds of a timestamp").
- `CAST('2026-10-01T12:00:00+05:30' AS TIMESTAMP)` gives `"2026-10-01
  06:30:00"` on 3.9.0, and fails with HTTP 400, `unsupported format`, on
  2.19.6 (recorded: "a timestamp with an offset").
- `NOW()` and `DATE_ADD` give the type `datetime` on 2.19.6 and `timestamp`
  on 3.9.0, with the same text in UTC. `TIMESTAMPDIFF` names the type
  `datetime` or `timestamp`, and gives the number 4 (recorded: "the
  functions of time"). So the type of that column does not fit its value.
- `INTERVAL 1 DAY` fails with HTTP 500, `InaccessibleObjectException`, so
  an interval cannot reach the client (recorded: "an interval"). `CAST(1
  AS BYTE)` and `CAST(1 AS SHORT)` fail with HTTP 400 (recorded: "a byte and
  a short").
- A `geo_point` is an object, `{"lat": 41.12, "lon": -71.34}`. Unlike
  Elasticsearch, which writes WKT.
- An `object` is a JSON object, `{"a": 1, "b": "x"}`, and `o.a` reads one
  field of it (recorded: "an object field by its path"). A `nested` field
  is always an array of objects, also for a document that holds one object.
  `n.a` gives the first nested object only, and `nested(n.a)` gives one row
  for each nested object (recorded: "a nested field by its path" and "the
  nested function"). Unlike Elasticsearch, which refuses an object and a
  nested field in a select.
- A field with several values in a document arrives as a JSON array under
  the type of its mapping: `[1, 2, 3]` as `integer`, `["a", "b"]` as
  `keyword`, `[1.5, null, 2.5]` as `double`, and two objects as `object`.
  `WHERE i = 2` finds that document (recorded: "a multi-valued field in a
  condition"). Unlike Elasticsearch, which refuses such a field unless
  `field_multi_value_leniency` is true.
- Text in the source that the mapping reads as a number or a boolean, such
  as `"42"`, `"1.5"` and `"true"`, arrives as the number or the boolean, and
  1.9 in a `long` arrives as 1 on 2.19.6, and as NULL on 3.9.0.

These facts were recorded about the types that SQL cannot read:

- A field of the mapping type `unsigned_long`, `wildcard`, `geo_shape`,
  `integer_range`, `flat_object`, `token_count`, `completion`,
  `search_as_you_type`, `percolator`, `join`, `rank_feature`,
  `rank_features`, `long_range`, `double_range`, `date_range`, `ip_range`,
  `xy_point` or `version` is left out of `SELECT *`, and naming it fails
  with HTTP 400, `can't resolve Symbol(namespace=FIELD_NAME, name=...)`
  (recorded: "a field of unsigned_long" and the requests after it, and
  "select a token_count" and the requests after it). A subfield of a text,
  such as `tk.raw`, fails the same way (recorded: "a field of a
  multi-field"). Unlike Elasticsearch, which reads `unsigned_long`,
  `geo_shape` and `version`.
- `match_only_text` arrives as `text`, `constant_keyword` as `keyword`, an
  `alias` as the type of its target, and `knn_vector` as `nested` with an
  array of numbers, on 3.9.0. On 2.19.6 each of these fails as above
  (recorded: "a field of match_only_text", "select a constant_keyword", "a
  field of alias" and "a field of knn_vector").
- 2.19.6 refuses the mapping type `version`. Both releases refuse
  `flattened`, `dense_vector`, `histogram`, `sparse_vector` and `semantic`,
  which are types of Elasticsearch (recorded: "make an index with one
  version" and the requests like it).

These facts were recorded about the legacy engine, which answered a
statement with `filter` (recorded: "every type on the legacy engine"):

- Its `SELECT *` names the columns in another order, gives `tk` twice, and
  names an `object` and a `nested` field `text` with an `alias` of `""`,
  while each value is an object or an array.
- On 2.19.6 it names `half_float`, `scaled_float` and `integer_range` as
  wire types. `integer_range` is `{"gte": 1, "lte": 5}`. On 3.9.0 it names
  them `float` and `integer_range`.
- On 2.19.6 it names a `date` field `date`, and sends the text of the
  source, such as `"2026-10-01T12:34:56.123+05:30"` and `"-1000-01-01"`, or
  the text that it formats, such as `"0001-12-30 00:00:00.000"` for
  `0001-01-01`, a day of the Julian calendar. On 3.9.0 it names the same
  field `timestamp`.
- It sends the text of the source for a number or a boolean that the source
  holds as text, such as `"42"` and `"true"`, under `integer` and
  `boolean`.

These facts were recorded about the types of an answer of a statement that is
not `SELECT`:

- `DESCRIBE TABLES` on 2.19.6 names every column of its answer `keyword`, and
  sends numbers in some of them, such as 10 in `NUM_PREC_RADIX` and 2 in
  `NULLABLE`. On 3.9.0 it names those columns `integer` and `short`
  (recorded: "describe tables"). So on 2.19.6 the driver fails the row with
  `dbimp.ErrInvalidValue` for the column `NUM_PREC_RADIX`, and `SHOW TABLES`
  reads (measured with `TestIntegrationSchema` on 2.19.6 and 3.9.0 on
  2026-10-07). See Open questions, 15.

These facts were measured on 3.9.0 on 2026-10-07 with the integration tests:

- The mapping type `semantic` exists on 3.9.0. It refuses a mapping with no
  model, with HTTP 400 and `model_id is required for the semantic field`. 3.8.0
  gave the same answer (measured on 3.8.0 and not measured again). So no
  test can make such a field, and SQL on it is not measured.
- A `GROUP BY` of 10050 groups gives 10000 groups with no sign, and so does a
  `DISTINCT`, which is the cap of D163 on the 3 series (`TestIntegrationDefaultCaps`).
  The setting `plugins.query.buckets` of 100 cut a `GROUP BY` of 300 groups to
  100 with no sign on 3.9.0 (recorded: "300 groups with a
  limit on groups of 100"), as it did on 3.8.0 (measured on 3.8.0 and not
  measured again). An earlier version of this line said that it did
  not cut, from a test on 3.9.0 that no longer exists. The recording is the
  evidence.

These facts come from the sources, and are not measured:

- The JDBC driver names the types `BOOLEAN`, `BYTE`, `SHORT`, `INTEGER`,
  `LONG`, `HALF_FLOAT`, `FLOAT`, `DOUBLE`, `SCALED_FLOAT`, `KEYWORD`,
  `TEXT`, `STRING`, `IP`, `NESTED`, `OBJECT`, `DATE`, `TIME`, `DATETIME`,
  `TIMESTAMP`, `BINARY`, `NULL`, `UNDEFINED`, `UNSUPPORTED` and `ARRAY`.
  It says that `VARBINARY`, `GEO_POINT` and `NESTED` are not fully
  supported.
- The legacy engine of 3.9.0 names an object `struct` in a page of a cursor
  (measured with `curl` on 2026-10-01).

## Parameters

These facts were recorded on each release:

- A statement names each parameter as `?`, and `parameters` holds an array
  of objects, each with a `type` and a `value`, in their order (recorded: "an
  integer parameter" and "a string parameter"). The JDBC driver sends this
  form (the JDBC driver, `JdbcQueryParam.java`).
- The server writes each value into the text of the statement before it
  parses it. The `name` of a column of a parameter is that text, such as
  `1.5` and `'2026-10-01'` (recorded: "a parameter of each type, with an
  alias for each").
- The types `integer`, `long`, `short`, `byte`, `double`, `float`,
  `boolean`, `string`, `keyword`, `date` and `null` work. A `float` arrives
  as a `double`, and a `date` as a `keyword`, because each is written as a
  number or a quoted string. The `long` 9223372036854775807 keeps every
  digit (recorded: "a parameter of each type, with an alias for each" and "a
  byte parameter").
- `timestamp`, `time`, `datetime` and a type that does not exist fail with
  HTTP 400, `Unsupported parameter type` (recorded: "a timestamp
  parameter", "a time parameter", "a datetime parameter" and "a parameter
  of a type that does not exist"). An `integer` whose value is text fails
  with HTTP 400, `Failed to parse PreparedStatement parameters` (recorded:
  "an integer parameter that is text").
- The string `it's a\b` arrives as `it\'s a\\b` on 2.19.6, so the server
  escapes the quote and the backslash and does not read the escapes back.
  On 3.9.0 it arrives as `it's a\b` (recorded: "a parameter of each type,
  with an alias for each"). A string with a quote and `OR 1=1 --` did not
  change the statement (recorded: "a string parameter that tries to
  inject").
- Too few values fail with HTTP 500, `Placeholder count is greater than
  parameter number`. A value too many is ignored (recorded: "too few
  parameters" and "too many parameters").
- A `?` with no `parameters` fails with HTTP 400 in the parser of the
  legacy engine. A `?` in a string literal is not a parameter (recorded: "a
  placeholder with no parameters" and "a question mark in a string").
- A plain value in place of an object fails with HTTP 500 on 2.19.6 and
  HTTP 400 on 3.9.0. `:p` fails with HTTP 400 (recorded: "a parameter with
  no type" and "a named parameter"). So there are no named parameters.
- Parameters work with `fetch_size`, and the answer has a cursor (recorded:
  "parameters with a fetch size").
- Unlike Elasticsearch, whose `params` takes plain values and refuses an
  object with a type.
- D168 decides that the driver sends no `parameters`. It writes each argument
  into the statement as a literal, with the parser for placeholders of the root
  package (D34). A string is in single quotes with each quote doubled, a
  backslash is itself, a `time.Time` is `TIMESTAMP '...'` in UTC with every
  digit, a `dbimp.Date` is `DATE '...'`, a `dbimp.LocalTime` is `TIME '...'`,
  and NULL is `NULL`. A `[]byte` and a `*apd.Decimal` fail, because SQL has no
  binary literal and no decimal type, and so does a `uint64` above the range
  of a long, `NaN` and an infinity. A named argument fails with
  `dbimp.ErrArguments` (measured: `TestIntegrationLiterals`, which sends 24
  values and a string with a quote, a backslash and an injection to both
  releases, as both principals, on 2026-10-07).
- Two arguments of one value in one `SELECT` fail on the server, because the
  name of a column is the text of its literal (recorded: "two parameters of
  one value"). A caller gives each an alias.

## Transactions

- OpenSearch has no transactions. `BEGIN` fails with HTTP 400, `Query must
  start with SELECT, DELETE, SHOW or DESCRIBE` (recorded: "begin").
- A write goes through the document API, and acts on one document, or on
  each line of a bulk request (recorded: "fill the index of every type").
  SQL sees it after a refresh, which the setup asks for with
  `refresh=true`.

## Errors

These facts were recorded on each release:

- An error of the SQL plugin is a JSON object, `{"error": {"reason": ...,
  "details": ..., "type": ...}, "status": ...}`, with `Content-Type:
  text/plain` (recorded: "a syntax error"). `type` is the name of a Java
  exception, such as `SQLFeatureNotSupportedException`,
  `SemanticCheckException` or `NullPointerException`. An error of the
  search engine itself, such as a refusal of the security plugin for `GET
  /`, has the form `{"error": {"root_cause": [...], "type": ...,
  "reason": ...}, "status": ...}` with `Content-Type: application/json`
  (recorded as `dbmeta_user`: "the version").
- A statement that the parser refuses gives HTTP 400, an unknown column HTTP
  400 with `SemanticCheckException`, a function with the wrong type HTTP 400
  with `ExpressionEvaluationException`, and an unknown index HTTP 404 with
  `IndexNotFoundException` (recorded: "a syntax error", "an unknown column",
  "a function with the wrong type" and "an unknown index").
- A cast that fails in a row fails the whole statement, before any row,
  with HTTP 400 and `NumberFormatException` (recorded: "a cast that fails
  in a row").
- An error after some rows arrives on a later page. With `fetch_size` 100,
  the first page of 100 rows came, and the second page failed with HTTP
  400, `For input string: "x121"`, at the row 121 (recorded: "a cursor
  whose second page fails" and "the second page that fails"). The point in
  time of that cursor stayed open (recorded: "the open cursors after the
  failed page").
- Some errors come with HTTP 200, and the status is only in the body. `a
  union` and `MULTI_VALUE` gave HTTP 200 with `"status": 500` (recorded: "a
  union" and, on 3.9.0, "the function multi_value"). A refusal of the
  security plugin in the legacy engine gave HTTP 200 with `"status": 403`:
  on 2.19.6 for a statement with `filter`, for `SELECT *` on the legacy
  engine and for a join, and on 3.9.0 for `SHOW TABLES` and `DESCRIBE
  TABLES` (recorded as `dbmeta_user`: "a statement with a filter of the
  query DSL", "every type on the legacy engine", "a join", "show tables"
  and "describe tables"). On 2.19.6 such a refusal also came with HTTP 500
  and `"status": 403` (recorded as `dbmeta_user`: "an index of the
  system").
- A body with no `query` fails with HTTP 500 on 2.19.6 and HTTP 400 on
  3.9.0 (recorded: "a body with no query").
- A wrong password gives HTTP 401 with a body of plain text (recorded: "a
  wrong password").
- No answer was HTTP 429. No request failed before it reached the server.
- Unlike Elasticsearch, which never sent an error with HTTP 200.

## Cancellation and timeouts

These facts were recorded on each release:

- The SQL plugin has no path to cancel a statement. `POST
  /_plugins/_sql/_cancel` fails with HTTP 400, `no handler found` (recorded:
  "a cancel endpoint of the SQL plugin").
- `POST /_plugins/_async_query` needs a data source, such as Spark on S3,
  and fails with HTTP 400, `DataSourceNotFoundException`, on a cluster that
  has none (recorded: "an async query").
- A cursor that the client leaves keeps its point in time open until its
  `keep_alive` of one minute ends. `GET /_search/point_in_time/_all` lists
  it (recorded: "a cursor that the client leaves" and "the open cursors
  after the client left"). `POST /_plugins/_sql/close` frees it (Paging,
  under Responses).
- `GET /_tasks?actions=*search*` listed no task after the statement ended
  (recorded: "the tasks of a search").

These facts were measured on 2.19.6 and 3.9.0 on 2026-10-07 with
`TestIntegrationCancel`. A statement with a `filter` that holds a script runs
for 2.9 seconds on 300 rows, and the server runs it in the legacy engine. When
the client left after 0.3 seconds, the search tasks of the server ended 2.9
seconds after the start, so the server did not stop the statement when the
client left, on either release. The driver returned the error of the context
at once, and the next statement on the same database worked.

These facts come from the sources, and are not measured:

- A running search can be cancelled with `POST /_tasks/<id>/_cancel`
  (Gemini and DeepSeek). The driver does not call it. Whether the ordinary
  user can call it was not measured. See Open questions, 10.

## Statements

These facts were recorded on each release:

- Two statements in one text fail with HTTP 400, `Illegal SQL expression`.
  A `;` at the end of one statement works (recorded: "two statements in one
  text" and "a statement that ends with a semicolon"). Unlike
  Elasticsearch, which refuses the `;` at the end.
- `--` and `/* */` comments work (recorded: "comments"). An empty statement
  fails with HTTP 400, `NullPointerException` (recorded: "an empty
  statement").
- A join, a subquery in `FROM`, a window function and a quoted index
  pattern such as `dbmeta_r*` work (recorded: "a join", "a subquery", "a
  window function" and "an index pattern"). `UNION` fails (Errors).
  Unlike Elasticsearch, which refuses a join.
- `MATCH`, `MATCH_PHRASE`, `MULTI_MATCH`, `QUERY`, `SIMPLE_QUERY_STRING`,
  `WILDCARD_QUERY` and `_score` work (recorded: "a full text match", "a
  match phrase", "a multi match", "a query string", "a simple query
  string", "a wildcard query" and "a score").
- `SHOW TABLES LIKE dbmeta%` gives one row for each index, with the columns
  of JDBC from `TABLE_CAT` to `REF_GENERATION`. `TABLE_CAT` is the name of
  the cluster, and `TABLE_TYPE` is `BASE TABLE`. `DESCRIBE TABLES LIKE
  dbmeta_rows` gives one row for each field (recorded: "show tables" and
  "describe tables").
- `1e308*10` reads as `1e308` with the alias `*10`, and gives 1e+308
  (recorded: "a division by zero and an overflow").

## Principals

These facts were recorded as `dbmeta_user` on each release:

- The ordinary user runs SQL on the indices `dbmeta*` (recorded: "a
  statement"). `GET /_plugins/_security/authinfo` names its roles:
  `dbmeta_role` on each release, and `own_index` too on 2.19.6 (recorded:
  "who am I").
- An index outside `dbmeta*` fails with a refusal for
  `indices:admin/mappings/get` (recorded: "an index of the system"). See
  Errors for its status.
- `SHOW TABLES LIKE %` fails for `indices:admin/get` (recorded: "show every
  table"). `DESCRIBE TABLES` works on 2.19.6, and fails on 3.9.0 (recorded:
  "describe tables").
- A cursor fails on 2.19.6 with HTTP 403, `no permissions for
  [indices:data/read/search]`, and works on 3.9.0 (recorded: "a cursor as
  the ordinary user"). On 2.19.6 the cursor opens a point in time, whose
  search names no index, so the grant on `dbmeta*` does not cover it. A user
  with `indices:data/read/search` on `*` paged (measured with `curl` and
  a role of its own on 2.19.6 on 2026-10-01, and then removed).
- A join fails on 2.19.6 for `indices:admin/aliases/get`, and works on
  3.9.0 (recorded: "a join").
- PPL fails with HTTP 403 for `cluster:admin/opensearch/ppl` (recorded: "a
  statement in PPL").
- `GET /` and `GET /_cat/plugins` fail with HTTP 403 for
  `cluster:monitor/main` and `cluster:monitor/state` (recorded: "the
  version" and "the version of the plugins"). SQL has no function for the
  version: `SELECT VERSION()` fails with HTTP 400 for both principals
  (recorded: "the version in SQL"). On 3.9.0 the header
  `X-OpenSearch-Version` gives the version to the ordinary user with every
  answer (Requests).

These facts were measured on 2.19.6 and 3.9.0 on 2026-10-07, as each
principal, with the integration tests:

- Every plain `SELECT` of the driver carries a page size, so the ordinary
  user of 2.19.6 gets HTTP 403 for `indices:data/read/search` for each one,
  until the caller turns the page size off with `WithParameter("fetch_size", 0)`.
  The result is then cut at the size limit of the server with no sign. A
  `SELECT` with a `LIMIT`, a `GROUP BY` or an aggregate carries no page size,
  and reads. The ordinary user of 3.9.0 pages (`TestIntegrationPages`).
- `SELECT VERSION()` fails with HTTP 400 for both principals on both releases,
  and `GET /` is refused to the ordinary user with HTTP 403 (`TestIntegrationVersion`).
  `usql` has no driver for OpenSearch, so it runs no statement for the
  version, and the answers of step 16 are these. The 3 series sends the header
  `X-OpenSearch-Version` with each answer, also to the ordinary user, and the 2
  series sends none.

## Flavors

- OpenSearch forked from Elasticsearch 7.10, and gets a driver of its own
  (D162). Elasticsearch has no `/_plugins/_sql` (recorded in
  `testdata/elasticsearch/`: "the SQL path of OpenSearch").
- 2.19.6 and 3.9.0 differ in the engine. 3.9.0 runs the Calcite engine,
  which reads every group of a `GROUP BY`, refuses the `json` format, reads
  `match_only_text`, `constant_keyword`, `alias` and `knn_vector`, and
  names `datetime` as `timestamp` (Responses and Types).
- Both releases keep the legacy engine, which answers a statement that the
  new engine cannot take, or a body with `filter`, or `LIMIT` with
  `fetch_size`. It differs in its types, its errors and its pages
  (Responses, Types and Errors).
- `GET /` names the product in `version.distribution`, `opensearch`, and the
  release in `version.number`, to the administrator only (recorded: "the
  version").
- PPL is a second language at `POST /_plugins/_ppl`. It answers in the same
  form, with no `status`, and names a `keyword` `string`, and on 3.9.0 an
  `integer` `int` (recorded: "a statement in PPL"). The driver does not
  speak it.
- Amazon OpenSearch Service and OpenSearch Serverless have the same path,
  and can need AWS Signature Version 4 (Gemini and DeepSeek, not measured).
  Open Distro for Elasticsearch had `/_opendistro/_sql` (Gemini), which
  2.19.6 still answers (Requests).

## Interfaces

The code writes this table (step 10). Run `DBIMP_UPDATE=1 go test -run
TestTables ./opensearch` to write it.

<!-- dbimp:interfaces -->
| Interface | Implemented | Reason |
| --- | --- | --- |
| `driver.DriverContext` | yes | OpenConnector parses the DSN once, for every connection. |
| `driver.Connector` | yes | The connector owns the transport, which every connection shares. |
| `io.Closer on the connector` | yes | Close closes the idle connections of the transport. |
| `driver.Pinger` | yes | Ping runs SELECT 1, which checks the credentials as each principal. GET / needs a cluster privilege that the ordinary user lacks. |
| `driver.SessionResetter` | no | A connection holds nothing on the server, because the SQL plugin has no sessions. |
| `driver.Validator` | no | A connection holds nothing on the server, so it is always valid. |
| `driver.NamedValueChecker` | yes | It keeps an Option and a uint64, which the driver refuses above the range of a long (D168), and hands every other value to database/sql. |
| `driver.QueryerContext` | yes | The driver writes each argument into the statement as a literal, because the server writes each value into the text itself (D168). |
| `driver.ExecerContext` | yes | Exec reads the result to its end. SQL takes no write, so RowsAffected fails (D163). |
| `driver.ConnPrepareContext` | yes | A prepared statement runs as its text, with its arguments, each time. |
| `driver.ConnBeginTx` | yes | BeginTx fails with dbimp.ErrNotSupported, because OpenSearch has no transactions (D168). |
| `driver.RowsColumnScanner` | yes | A value is decoded when its row is read, and assigned when it is scanned. |
| `driver.RowsNextResultSet` | no | A request holds one statement, and its pages are one result. |
| `driver.RowsColumnTypeScanType` | yes | The answer names the type of each column (D168). |
| `driver.RowsColumnTypeDatabaseTypeName` | yes | The answer names the type of each column, which the driver writes in upper case. |
| `driver.RowsColumnTypeLength` | no | The answer names no length. |
| `driver.RowsColumnTypeNullable` | yes | The answer does not say whether a column can be NULL, and every type can be. |
| `driver.RowsColumnTypePrecisionScale` | no | The answer names no precision and no scale, and there is no decimal type. |
<!-- /dbimp:interfaces -->

## Faults

`usql` has no driver for OpenSearch, so no driver of `usql` has faults to
avoid. The other drivers that step 5a read have these, which this driver
must not repeat:

- The Go client decodes `datarows` as `[][]json.RawMessage`, and `schema`
  as `[]json.RawMessage`, so it holds the whole result in memory and leaves
  each value as the text of JSON (the Go client, `QueryResp`).
- The JDBC driver says that it does not fully support `VARBINARY`,
  `GEO_POINT` and `NESTED` (the JDBC driver, `OpenSearchType.java`).

This driver does none of them. It reads each page one token at a time, and
reads the next page only when `Rows.Next` needs it (`TestLargeResult` reads a
result of 64 MiB in 128 pages, and the heap stays under 16 MiB). It decodes
each value by the type of its column, so it gives a `geo_point` as a map, a
`nested` field as a list of maps, and a `binary` value as bytes. It never
holds the JSON text of a value.

## Second opinions

Step 5a asked Gemini and DeepSeek the four questions on 2026-10-01. Step 7
asked both what step 6 did not find on 2026-10-02, and each lead went to
the server. A lead that the server did not show stays "not measured".

- CRUD. Gemini said that SQL reads only and refuses `DELETE`. DeepSeek said
  that the legacy engine runs `DELETE`. The server refuses it, because
  `plugins.sql.delete.enabled` is false, and says that it will be
  deprecated.
- Schema. Both said that SQL has no DDL, and that `SHOW TABLES` and
  `DESCRIBE TABLES` work. The server agrees.
- Statements. Gemini said that `UNION` works. It fails with HTTP 200 and an
  error. Gemini named `MULTI_VALUE` for a field with several values. It
  fails. DeepSeek named joins, subqueries and window functions, and each
  works. DeepSeek named user functions. `CREATE FUNCTION` fails.
- Formats. Gemini named `tsv`. It fails on each release. Both named `json`,
  which 3.9.0 refuses.
- Types. Both said that `half_float` reads as `float`, `scaled_float` as
  `double`, and `date_nanos` as `timestamp`. The server agrees. Gemini said
  that `wildcard` and `constant_keyword` read as `keyword`. `wildcard` fails,
  and `constant_keyword` reads as `keyword` on 3.9.0 only. DeepSeek named
  `unsigned_long`, the range types, `flattened`, `token_count`,
  `completion`, `search_as_you_type`, `percolator`, `join`,
  `rank_feature`, `rank_features`, `histogram`, `dense_vector` and
  `sparse_vector`. SQL reads none of them, and the last four, with
  `flattened`, are not mapping types of OpenSearch. Gemini said that
  `geo_shape` and `knn_vector` read. `geo_shape` fails, and `knn_vector`
  reads as `nested` on 3.9.0 only. DeepSeek said that a field with several
  values arrives as an array. It does.
- Parameters. Gemini said that `?` takes the types `byte`, `short`,
  `integer`, `long`, `float`, `double`, `string`, `boolean`, `date`,
  `time` and `timestamp`. The last two fail. DeepSeek proposed `:p`, `$1`
  and plain values. `:p` and plain values fail, and `$1` was not sent.
- Transactions. Both said that there are none. `BEGIN` fails.
- Paging. Gemini proposed the setting `plugins.sql.query.bucket.size`, and
  DeepSeek `plugins.sql.group_by.limit`. Neither exists. Gemini said that
  the Calcite engine reads a `GROUP BY` past 1000 groups. 3.9.0 does, and
  `plugins.query.buckets` caps it. Both said that a `GROUP BY` with
  `fetch_size` can page. It goes to the legacy engine, and gives 200 groups
  with no cursor. Both said that an error can arrive on a later page. It
  does. Gemini said that a cursor ends after its `keep_alive`. A cursor
  with a `keep_alive` of 2 seconds still answered after 8.
- Cancel. Both proposed `POST /_tasks/<id>/_cancel`, not measured. DeepSeek
  proposed `POST /_plugins/_sql/_cancel` and `wait_for_completion_timeout`.
  Both fail. Gemini proposed `POST /_plugins/_async_query`. It needs a data
  source of Spark.
- Flavors. Both named Amazon OpenSearch Service with AWS Signature Version
  4, not measured. Gemini named the path of Open Distro, which 2.19.6
  answers and 3.9.0 does not.

Step 8a asked both models on 2026-10-02 to review the mapping of the types
against [TYPES.md](TYPES.md) and D135:

- Both agreed with the integers, the floats, the strings, the binary, the
  date, the time of day, the object as a map and the nested field as an
  array, and with the geometry as a map with `lat` and `lon`. Gemini said
  that a struct with `Lat` and `Lon` is more idiomatic.
- Both said that a `timestamp` is an instant, a `time.Time` in UTC, and not
  a local timestamp, because the server writes an offset as UTC. The table
  follows them.
- Both said that `datetime` of 2.19.6 is the `timestamp` of 3.9.0, and
  gets the same Go type, so that one statement gives one Go type on both
  releases. The table follows them, where the first proposal was a local
  timestamp.
- Both said that the kind range gives the text of the server, so
  `integer_range` is a `string`. The table keeps `map[string]any`, because
  the server sends an object, and hard rule 3 refuses the text of JSON in
  place of a value. This waits for Ken.
- On a field with several values, Gemini said `[]any`, because the first
  value loses data and an error breaks a query of a common document.
  DeepSeek said an error, because a JSON array is not the type that the
  column names, unless a decision names a form for it.
- On the legacy `date` that holds the text of a timestamp, Gemini said that
  the driver takes its day, and DeepSeek said that it is an error, unless a
  decision names another form.

## Open questions

Ken decided the questions of step 8a and step 9 on 2026-10-02, and D168 holds
the answers. The questions that D168 answers are these:

1. The mapping of the types is the table in Types. A `timestamp` and a
   `datetime` are a `time.Time` in UTC, and a `geo_point` and an
   `integer_range` are a `map[string]any` (D168, item 3).
2. A field with several values is a `[]any` (D168, item 3).
3. The cut of a result. The driver goes ahead, sends a page size with each
   plain `SELECT`, and documents the caps of the server (D163 and D168, item 7).
   Statements and releases that the page size does not reach keep the cap:
   a `LIMIT`, a `GROUP BY`, a `DISTINCT` and an aggregate (Responses, and
   `TestIntegrationDefaultCaps`).
4. A plain `SELECT` carries `fetch_size` and every other statement carries
   none (D168, item 7). See 12 for the meaning of "plain".
5. The legacy engine. D168 names no form for its answers. See 15.
6. An error that arrives with HTTP 200 reads its status from the body
   (D168, item 7).
7. The ordinary user of 2.19.6 cannot page. D168 says that this waits for
   `dbmeta`. See 13.
8. The release. Question 17 closes it. The driver sends no request for the
   release (D178, item 11, which amends D168, item 10).
9. The driver writes each argument as a literal (D168, item 5).
10. A statement that runs long. The measurement is in Cancellation and
    timeouts: the server runs it to its end after the client leaves. The
    driver closes the request and the cursor only (D168, item 8). Ken decided
    on 2026-10-07 that the driver does not call `POST /_tasks/<id>/_cancel`
    (D178, item 6).

These questions came from steps 10 to 17a. Ken closed questions 11, 13, 14 and
17 on 2026-10-07 (D178). Questions 12, 15, 16 and 18 still wait for Ken:

11. The rows of this driver keep the context of the statement, because the
    request for each next page needs it (`opensearch/rows.go`). The driver
    stores a context nowhere else. Ken decided on 2026-10-07 that hard rule 4
    of AGENTS.md names the rows of an OpenSearch query (D178, item 2). This is
    the same question as 6 of ELASTICSEARCH.md.
12. Closed by D178. D168 says that each plain `SELECT` sends `fetch_size`, and does not say
    what plain means. The driver reads the statement without its literals,
    quoted names and comments. It is plain when it starts with `SELECT`, has a
    `FROM`, and has none of the words `LIMIT`, `GROUP`, `DISTINCT`, `JOIN`,
    `UNION`, `MINUS`, `INTERSECT`, `HAVING` and `OVER`, no second `SELECT`, and
    no call of `COUNT`, `SUM`, `AVG`, `MIN`, `MAX` or `NESTED`. The server runs
    each of those in the legacy engine when it has a page size, and the
    legacy engine gives 200 groups with no cursor, a `double` for a count and for
    a keyword, and no schema on a later page (`plainSelect` in
    `opensearch/statement.go`, and `TestFetchSizeOfAPlainSelect`). A statement in
    doubt gets no page size and the server cuts its result at the size limit.
    The other choice is to send the page size to every `SELECT` and accept the
    legacy engine, or to never send it.
13. The ordinary user of 2.19.6 gets HTTP 403 for each plain `SELECT`, because
    the page size opens a cursor and the cursor needs `indices:data/read/search`
    on every index (Principals). Ken decided on 2026-10-07 that the driver
    returns this error, does not send the statement again, and always sends a
    page size to a plain `SELECT` (D178, item 5). The caller turns paging off
    with `WithParameter("fetch_size", 0)`, and then reads at most 10000 rows with
    no sign.
14. The driver does not close the cursor of a page that the caller leaves
    after the context ended in the middle of the page, because it learns the
    cursor only at the end of the page. The server drops it when its
    `keep_alive` of one minute ends (Responses). The driver closes each cursor
    that it knows, including one that it read before the context ended
    (`TestCancelAfterTheCursor`). Ken decided on 2026-10-07 that this is the
    behavior (D178, item 6).
15. Closed by D178. A value that does not fit its type. The legacy engine names an object and a
    nested field `text` with an empty alias and sends an object or an array,
    sends a `date` as the text of a timestamp on 2.19.6, sends a number or a
    boolean that the source holds as text under `integer` and `boolean`, and
    names a keyword in a `GROUP BY` `double`. `DESCRIBE TABLES` on 2.19.6 names
    every column `keyword` and sends numbers (Types). D168 names no form, and
    hard rule 3 forbids text in place of a value, so the driver fails the row with
    `dbimp.ErrInvalidValue` and the name of the column
    (`TestLegacyObjectInATextColumn`, `TestReplayLegacyGroupBy`). The statements
    that reach it are one with a `filter`, one with a `LIMIT` that a caller gives
    a page size with `WithParameter`, and `DESCRIBE TABLES` on 2.19.6. The other
    choices are the decoded JSON value, or a conversion by the value.
16. Closed by D178. `WithTimeout` fails with `dbimp.ErrNotSupported` for a time above zero, as
    D109 says for an option that the server cannot honor. The SQL plugin has no
    time limit, and `wait_for_completion_timeout` does not work (Requests).
    `WithDatabase` fails the same way. `WithParameter` applies to the first
    request of a statement and not to the request for each next page, which holds
    the cursor only.
17. `dbrun` starts 2.19.6 and 3.9.0 now, and `dbmeta` v0.3.0 and later no longer
    lists 3.8.0. Step 6 was recorded again on 3.9.0 on 2026-10-07, and the
    recordings of 3.8.0 were deleted. Ken decided this (D178, item 7). The
    integration tests ran on 2.19.6 and 3.9.0, and the replay tests read the
    recordings of 2.19.6 and 3.9.0. 3.9.0 differs from 3.8.0 in the facts under
    Types, the setting `plugins.sql.complex_worker_pool.enabled`, and the index
    `.plugins-ml-config`. The driver sends no request for the release, because
    no answer that it reads differs between releases: the types of an answer
    name themselves, and the legacy engine is reached by the statement and not
    by the release (D178, item 11).
18. Steps 16 and 20 send three requests, which wait for the release (W29 in
    [BACKLOG.md](BACKLOG.md)). The `url` that `dbrun` prints already has the
    scheme `opensearch`, so the tests need no conversion, and the workflow
    needs no change but its comment.

## Integration tests

The integration tests of the driver read `OPENSEARCH_DSN`, and
`OPENSEARCH_ORDINARY_DSN` for the ordinary user, and skip when
`OPENSEARCH_DSN` is empty. Each is the `url` of `dbrun`. The tests ran on
2026-10-07, one server at a time, with `-race`, on a fresh container of each
release:

| Release | Passed | Skipped | Failed |
| --- | --- | --- | --- |
| `opensearch-2.19.6` | 347 | 34 | 0 |
| `opensearch-3.9.0` | 361 | 20 | 0 |

A count is a test or a subtest, and each principal is a subtest. A test skips
for a difference between the releases, or for the ordinary user of 2.19.6, with
the reason in its log. The tests make their indices as the administrator, with
the name of the run as the prefix, and the prefix starts with `dbmeta` so that
the ordinary user can read it. `TestMain` removes every index of the run, also
when a test fails. SQL takes no write, so the administrator writes through the
document API, and each principal reads through the driver. The ordinary user
of 2.19.6 reads with `WithParameter("fetch_size", 0)`, because it cannot page
(Open questions, 13).

The round trip stores every type that `features.json` marks yes in a field of
that type, with the values of DRIVER.md: NULL, the zero value, the smallest and
the largest value, an empty value, a long value, text outside ASCII, and the
last digit of precision. It stores a value as a bound argument, then updates,
reads and deletes it. It cannot store a value as a literal, because SQL has no
`INSERT`, and each literal is logged as skipped inside the test
(`TestIntegrationRoundTrip`). `TestIntegrationLiterals` holds the literals of a
query instead. These types need a note on how the round trip stores them:

- The type `undefined` has no field. The select is `SELECT NULL AS v`, with a
  row that stores a NULL in a `keyword` field.
- A `date` is a `date` field with the format `yyyy-MM-dd`, and a `time` is a
  `date` field with the format `HH:mm:ss`. A `timestamp` is a `date` field with
  milliseconds, and a `datetime` is a `date_nanos` field, to keep every
  nanosecond.
- A `half_float`, a `scaled_float` and an `integer_range` are named only by the
  legacy engine (Types), so their select carries the filter `match_all`, which the
  ordinary user of 2.19.6 cannot use. Their tests skip for that principal.
- A `binary` value is stored as base64, and read as bytes.
- The types that `features.json` marks no have a test of the refusal of the
  server. SQL leaves a field of such a type out of `SELECT *`. A type that
  OpenSearch lacks, such as `dense_vector`, is refused by the mapping.

The tests also hold the statements of CRUD on three indices, the operations on
a schema, every feature of the survey, the pages and the cursor, the caps at
their defaults and with the size limit lowered, the cancel, the principals and
the version, as each principal. The ones that the driver does not speak, such
as the other formats of an answer, PPL and the list of tasks, go through the
HTTP API.


## Compared with Couchbase

Step 17a compares this driver with `couchbase`, the first driver (D97). It was
written on 2026-10-07 from the staged code. A fact of Couchbase comes from
[COUCHBASE.md](COUCHBASE.md), and a fact of OpenSearch from the sections above.

### The server

| | Couchbase | OpenSearch |
| --- | --- | --- |
| Request | `POST /query/service`, with `statement`, `args` and `$name` | `POST /_plugins/_sql`, with `query` and, for a plain `SELECT`, `fetch_size`. A later page sends `cursor` in place of `query` (Requests) |
| Database | The key `query_context` of the body | None. A statement names its indices (Statements) |
| Language | SQL++, which is close to SQL | SQL of its own, which reads only: `SELECT`, `SHOW` and `DESCRIBE`. `DELETE` is turned off (Requests) |
| DDL | In SQL++ | None. A write goes through the document API, and every statement of DDL is refused with HTTP 400 (Requests) |
| Parameters | `?`, `$1` and `$name` | `?` only, from an array of objects with a type and a value. The server writes each value into the text itself, and 2.19.6 changes a string with a quote or a backslash (Parameters) |
| Framing | One body for the whole result, which does not page | One object for each page, with a `cursor` for the next page. Each cursor serves once (Responses) |
| Columns | `signature`, before the first row | `schema`, before the first row, with the type of each column, and `alias` for the label that `AS` gives (Responses) |
| Order | The projection on 7.6 and 8.0, the names on 7.2 | The statement. `SELECT *` gives an order that is neither the mapping nor the names (Responses) |
| Errors | Can come with HTTP 200, after some rows | A JSON object with the exception of the server, with a status that is not 2xx, or with HTTP 200 and the status in the body. After some rows, the error is the answer to the request for a later page (Errors) |
| Types | JSON. No date, decimal, UUID or binary | JSON, with a type name for each column. A time is text with no zone, binary is base64 and a geometry is an object (Types) |
| Cancel | The server stops a query when the client leaves | The SQL plugin has no cancel, and the server runs a statement to its end when the client leaves. A cursor stays open until it is closed or its `keep_alive` ends (Cancellation and timeouts) |
| Transactions | `BEGIN WORK` in SQL++, carried by `txid` | None. `BEGIN` is refused with HTTP 400 (Transactions) |
| Authentication | Basic, or `creds` in the body | Basic. Amazon OpenSearch Service can need AWS Signature Version 4, which the driver does not sign (Requests) |
| Default port | 8093, or 18093 with TLS | 9200, with and without TLS |

The differences that a caller sees:

- Every write fails with the refusal of the server, because SQL takes none
  (D163).
- A statement has no database, and `WithDatabase` fails (D168).
- A result comes in pages, and an error on a later page fails the rows with
  `dbimp.ErrIncomplete` after exactly the rows that arrived (D168).
- The driver writes each argument into the statement as a literal, where
  Couchbase binds it on the server (D168, item 5).
- A statement with a `LIMIT`, a `GROUP BY`, a `DISTINCT` or an aggregate carries
  no page size, and the server cuts its result with no sign at the caps of D163
  (Open questions, 12).
- The server runs a statement on after the client leaves. The driver closes the
  request and the cursor, and cannot stop the work (D168, item 8).
- A field that holds several values is a `[]any` (D168, item 3).
- `WithTimeout` fails, because the SQL plugin has no time limit (Open
  questions, 16).

### The driver

| | `couchbase` | `opensearch` |
| --- | --- | --- |
| Size, without tests, on 2026-10-07 | About 1300 lines in 8 files | About 1700 lines in 9 files |
| `Config` | `QueryContext`, `ScanConsistency`, `Timeout`, `Durability`, `TxTimeout` | `Host`, `Port`, `TLS`, `User`, `Password`, `FetchSize` (D168) |
| Options for one statement | Six `With` options for one statement, through `WithOptions` or an argument, and two for `BeginTx`, through `WithOptions` only (D40, D46 and D109). `WithDatabase` sets `query_context`, as `WithQueryContext` does. `WithParameter` sets any key of the body | `WithTimeout`, `WithReadonly`, `WithParameter`, `WithDatabase` and `WithFetchSize`, through `WithOptions` or an argument (D109). `WithTimeout` and `WithDatabase` give `dbimp.ErrNotSupported`. `WithReadonly` changes nothing, because every statement is read-only. `WithParameter` sets any key of the body of the first request, such as `filter`, and `fetch_size` with 0 turns paging off |
| Arguments | Sent to the server as `args` and `$name` | Written into the statement as literals by the parser for placeholders of the root package. A `[]byte`, a `*apd.Decimal`, a `uint64` above a long, `NaN` and an infinity are refused (`opensearch/statement.go` and `opensearch/conn.go`) |
| Rows | `dbimp.ObjectRows` from the root package, after the driver reads the signature | `dbimp.ArrayRows` for each page, and a reader of the members of the page around it, which follows the cursor (`opensearch/rows.go`) |
| Types of the columns | `ColumnTypeDatabaseTypeName` and `ColumnTypeScanType` from the signature, and `ColumnTypeNullable` | `ColumnTypeDatabaseTypeName`, the type in upper case, `ColumnTypeScanType` and `ColumnTypeNullable`. Every column can be NULL |
| Values | `int64`, `float64`, or `*apd.Decimal` for an integer too large for `int64`. Bytes are decoded from base64 (D44) | By the type of the column, as the type table says: `time.Time`, `dbimp.Date`, `dbimp.LocalTime`, `[]byte`, a map for a geometry, an object and a range, and a `[]any` for a nested field and for a field with several values (D135 and D168) |
| Result of `Exec` | `RowsAffected` from `metrics.mutationCount` | `RowsAffected` and `LastInsertId` return `dbimp.ErrNotSupported`, because SQL changes no rows (D163) |
| Transactions | `BeginTx` sends `BEGIN WORK`. `ReadOnly` sends `readonly` | `BeginTx` returns `dbimp.ErrNotSupported` (D168) |
| Reset of a session | `ResetSession`, which it keeps as a guard (D41 and D102), and `IsValid` | None. A connection holds nothing on the server |
| Cancel | The request carries the context, and `net/http` stops it when the context ends (D36 and D42) | The same. The server goes on with the statement. Rows that the caller closes early close the cursor with `POST /_plugins/_sql/close` and a limit of 5 seconds (D168) |
| Errors | `*ResponseError`, with the HTTP status, the status of the body, and a list of `Error{Code, Msg}` | `*Error{HTTPStatus, Status, Type, Reason, Details}`, which unwraps to `*dbimp.StatusError`, and to nothing for an error with HTTP 200 |
| Authentication | Basic | Basic. The driver follows no redirect, so the credentials go to the host of the DSN only (D168) |
| Other exports | The `With` options and `Option` | The `With` options and `Option`, `Error`, `Config`, `ParseDSN` and `NewConnector` |

The differences that a caller sees:

- A value keeps its type, a time and a date too, where Couchbase gives JSON
  shapes (D135 and D168).
- `RowsAffected` always returns an error, where Couchbase counts every
  statement by `mutationCount` (D163).
- `WithReadonly` succeeds and does nothing, where Couchbase sends `readonly`,
  because SQL takes no write (D163).
- `WithTimeout` fails, where Couchbase sends the time to the server (Open
  questions, 16).
- The rows keep the context of the statement for the next page, where the rows
  of Couchbase hold none (Open questions, 11).
- `Close` before the end can read the rest of the current page to close the
  cursor, at most 256 KiB and for at most 5 seconds (Open questions, 14).
- A statement with a page size of the driver fails for the ordinary user of
  2.19.6 (Open questions, 13).
