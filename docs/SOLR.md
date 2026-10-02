# Apache Solr

This document holds what is known about Apache Solr, for its driver (W27 and
D162). The headings are the template of [DRIVER.md](DRIVER.md).

Steps 5a to 7 measured Solr 9.9.0, 9.10.1 and 10.0.0 in SolrCloud mode, as
`admin` and as `dbmeta_user`, on 2026-10-01 and 2026-10-02. A fact marked
"recorded" is in `testdata/solr/`, and the name in quotes after it is the
name of its request in `requests.json` there. A fact marked "measured" names
how it was measured. The three releases gave the same answers to every SQL
statement. The few differences are under Flavors. Each section starts with
the measured facts. The facts after them come from the sources, and each one
that is not measured says so. The sources are these:

- "The SQL module" is `solr/modules/sql` of `github.com/apache/solr` at its
  main branch, read on 2026-10-01: `SQLHandler.java`, `SolrSchema.java` and
  `SolrTable.java`. It is the code that answers `/sql`.
- "The JDBC driver" is `solr/solrj-streaming/src/java/org/apache/solr/client/solrj/io/sql`
  of the same repository, read on 2026-10-01. It is the driver of SolrJ, in
  Java.
- "solr-go" is `github.com/stevenferrer/solr-go`, a Go client of the
  Collections API, the Schema API, the query API and the update handler,
  read on 2026-10-01. It sends no SQL.
- "Gemini" is `gemini-3.8-flash`, and "DeepSeek" is `deepseek-flash`, asked
  on 2026-10-01 for step 5a and on 2026-10-02 for step 7.
- "The dbmeta entry" is `container/solr.go` in `dbmeta`.

## Summary

- Apache Solr is a search server built on Apache Lucene. A collection holds
  documents, and each document holds fields. Parallel SQL reads a collection
  as a table, with Apache Calcite, and sends the work to the search handlers
  of Solr (the SQL module).
- R holds. `dbrun` starts `solr-9.9.0`, `solr-9.10.1` and `solr-10.0.0`, each
  in the Staged tier. 9.9.0 and 10.0.0 have the cadence `tested`, and 9.10.1
  the cadence `nightly` (the dbmeta entry). Each one runs SolrCloud with the
  ZooKeeper that Solr embeds, the module `sql`, and basic authentication
  (measured with `dbrun start` on 2026-10-01).
- H holds. A statement is `POST /solr/<collection>/sql`, and the answer is
  JSON (recorded: "a statement").
- S holds. The dialect is the SQL of Calcite, read only, with the lexical
  rules of MySQL (recorded, and the SQL module).
- The priority is in [TARGETS.md](TARGETS.md), under D162.
- `dburl` has no scheme for Solr, and `usql` has no driver for it (read in
  `../dburl/scheme.go` and `../usql/drivers/` on 2026-10-01). So no driver of
  `usql` is replaced.
- `dbmeta` has no model for Solr. Its entry makes the collection `dbmeta`
  with the configuration set `_default`, the administrator `admin`, and the
  ordinary user `dbmeta_user` with the role `search`, which can read a
  collection and run SQL on it (the dbmeta entry).
- SQL in Solr cannot write. `INSERT`, `UPDATE` and `DELETE` fail, and a
  write goes through the update handler, which is not SQL (recorded: "crud:
  sql insert", "crud: sql update", "crud: sql delete" and "crud: insert").
  DRIVER.md says to stop and ask Ken when a server refuses one of insert,
  update and delete. See the open questions.

## Requests

These facts were recorded on each release:

- A statement is the form field `stmt` of a `POST` to
  `/solr/<collection>/sql`, sent as `application/x-www-form-urlencoded`
  (recorded: "a statement"). `GET` with `stmt` in the query gives the same
  answer (recorded: "a statement by GET").
- The collection of the path only names the handler. A statement sent to
  `/solr/dbmeta/sql` reads the collection `dbimp` that its `FROM` names
  (recorded: "a statement sent to another collection"). The path of a
  collection that does not exist answers 404 with an HTML page on 9.x, 405 on
  10.0.0 for `admin`, and 403 for `dbmeta_user` (recorded: "a statement sent
  to a collection that does not exist").
- A body of JSON, such as `{"params": {"stmt": ...}}`, is not read. The
  answer is HTTP 200 with the error `stmt parameter cannot be null` (recorded:
  "a statement in a body of JSON"). A request with no `stmt` answers the same
  (recorded: "no statement").
- Authentication is HTTP basic. A request with a wrong password answers 401
  with an HTML page and `WWW-Authenticate: Basic realm="solr"` (recorded: "a
  wrong password"). A request with no credentials answers 401 (the dbmeta
  entry, whose check of readiness waits for that answer).
- `includeMetadata=true` adds a first tuple that names the columns
  (Responses). `aggregationMode` is `facet` or `map_reduce`, and changes how
  `GROUP BY` runs (recorded: "feature: a group by facet" and "feature: a group
  by map reduce"). `numWorkers=2` fails a `GROUP BY` in `map_reduce` with
  `IndexOutOfBoundsException` on this server of one node (recorded: "lead:
  two workers"). `timeAllowed=1` changes nothing (recorded: "lead: a time
  limit").
- The setup of `requests.json` makes the collection with the Configsets API
  (`/solr/admin/configs?action=CREATE&baseConfigSet=_default`), the
  Collections API (`/solr/admin/collections?action=CREATE`) and the Schema API
  (`POST /solr/dbimp/schema` with `add-field-type` and `add-field`), and adds
  the documents with `POST /solr/dbimp/update?commit=true` (recorded: "make
  the configuration set", "make the collection", "add the field types", "add
  the fields" and "add the documents of every type"). Each takes a few
  hundred milliseconds.
- On a server that deleted a collection, a new collection of the same name
  can fail with `Core with name 'dbimp_shard1_replica_n1' already exists`, or
  later with `Lock held by this virtual machine`. Solr keeps a listener for
  the configuration set of the deleted core. When a configuration set of
  that name is made again, the listener fails to reload the old core, and
  the core stays in `initFailures`. Only a restart of Solr cleared the lock
  (measured on 10.0.0 on 2026-10-01, from `solr.log`). So the setup unloads
  that core, and each release was recorded on a fresh start.

These facts come from the sources, and are not measured:

- The handler takes the connection parameters of Calcite that it allows,
  then sets `lex` to `MYSQL` and `zk` to its own ZooKeeper, so a caller
  cannot change either. `numWorkers` defaults to 1, `aggregationMode` to
  `facet`, and `includeMetadata` to false (the SQL module).
- The JDBC driver sends `stmt` and every property of its connection as
  parameters to `/sql` of a replica that it picks at random (the JDBC
  driver).
- `workerCollection` and `workerZkhost` name where `map_reduce` runs
  (Gemini and DeepSeek, not measured).

## The DSN

- Step 9 decides the URL (D27 and D35). `dburl` has no scheme to follow.
- The dbmeta entry gives each principal as `http://user:password@host:port`,
  with `admin` and `dbmeta_user`, and the port 8983 in the container. The
  path `/solr` is part of every request.
- A statement needs a collection in its path (Requests), and any collection
  that the user can read serves. Its `FROM` names the table.

## Responses

These facts were recorded on each release:

- The answer is `{"result-set": {"docs": [...]}}`. Each row is an object.
  The last element is the EOF tuple, `{"EOF": true, "RESPONSE_TIME": n}`
  (recorded: "a statement").
- The keys of a row are in the order of the `SELECT`, and a field with no
  value is `null` (recorded: "the order of the columns"). A key has the case
  that the statement wrote: `SELECT ID, N_I FROM DBIMP` gives the keys `ID`
  and `N_I`, and names are matched without regard to case (recorded: "names in
  another case").
- With no rows and no `includeMetadata`, the answer holds only the EOF tuple,
  so it names no column (recorded: "no rows, without the metadata").
- With `includeMetadata=true`, the first tuple is `{"isMetadata": true,
  "fields": [...], "aliases": {...}}`. `fields` names the field of each
  column in order, and `aliases` maps each field to the name of its column.
  It comes with no rows too (recorded: "the metadata of the columns" and "no
  rows, with the metadata"). It names no type.
- An aggregate with no alias is named `EXPR$0`, `EXPR$1` and so on (recorded:
  "an aggregate with no name").
- Two columns with one name are wrong. `SELECT id AS a, id AS b, n_i AS b`
  gives `fields` `["id", "id", "n_i"]`, `aliases` `{"n_i": "b", "id": "b"}`,
  and rows of three keys `b`, each with the value of `n_i` (recorded: "two
  columns with one name").
- `SELECT *` names every field of the schema, and the pseudo fields
  `_query_` and `score` (recorded: "every field").
- A statement with no `LIMIT` reads every document through the export
  handler, in no order that the statement names: 303 rows of 303 (recorded:
  "a result with no limit"). Each field must have docValues. A `TextField`
  fails with `must have DocValues to use this feature`, a `SortableTextField`
  needs `useDocValuesAsStored`, and a `UUIDField` or a location fails with
  `Export fields must be one of the following types:
  int,float,long,double,string,date,boolean,SortableText` (recorded: "a text
  field with no limit", "a sortable text field with no limit" and "a uuid and
  a location with no limit"). `score` with no limit fails (recorded: "score
  with no limit").
- A statement with `LIMIT` reads through the select handler. `LIMIT 1000` of
  303 documents gave 303 rows. `LIMIT 5 OFFSET 5` and `OFFSET 5 ROWS FETCH
  NEXT 5 ROWS ONLY` gave the sixth to the tenth (recorded: "a limit above the
  rows", "a limit and an offset" and "fetch next rows"). `OFFSET` with no
  `LIMIT` fails with `OFFSET without LIMIT not supported by Solr!` (recorded:
  "an offset with no limit").
- A `GROUP BY` in `facet` mode gave all 302 groups, ordered by the value, also
  with `ORDER BY count(*) DESC` (recorded: "a group of each value" and "lead:
  a group ordered by its count, with no limit"). In `map_reduce` mode, a
  `GROUP BY` with `ORDER BY count(*) DESC` and no `LIMIT` gave 100 groups of
  301, with no sign that it cut the result (recorded: "lead: a group ordered by
  its count, map reduce"). `DISTINCT` gave all 302 values (recorded: "distinct
  values").
- There is no cursor and no next page. One body holds the whole result. A
  large answer has no `Content-Length`, so it streams, and a small one has
  one (recorded: "a result with no limit" and "a statement").
- With `Accept-Encoding: gzip`, an answer of 300 rows came as gzip, and
  `Vary: Accept-Encoding` is on every answer (recorded: "a gzip answer").
- `GET /solr` redirects to `/solr/`, with 302 on 9.x and 301 on 10.0.0
  (recorded: "a path with no slash"). No answer to `/sql` redirected.
- No answer was HTTP 429.

These facts come from the sources, and are not measured:

- With no limit, the table reads `/export`. With a limit, it reads `/select`
  with `rows` set to the limit and the offset (the SQL module).
- In `facet` mode with an order by a metric, the facet has a default limit
  of 100 (Gemini and DeepSeek). The server showed the cut in `map_reduce`
  mode and not in `facet` mode.

## Types

The column Kind names the kind of each type in [TYPES.md](TYPES.md), which
maps every kind onto its Go type (D135 and D137). Step 8a wrote this table
on 2026-10-02, and Ken has not reviewed it yet. A row names the class of the
field type in the schema of Solr, because the result names none. The Go
type and the database type assume that the driver learns the SQL type of
each column from `metadata.COLUMNS` (below). That is a choice of step 9.

<!-- dbimp:types -->
| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |
| --- | --- | --- | --- | --- | --- |
| null | null | `nil` | `interface {}` | `` | yes |
| StrField | string | `string` | `string` | `VARCHAR` | yes |
| TextField | string | `string, the stored text` | `string` | `VARCHAR` | yes |
| SortableTextField | string | `string, the stored text` | `string` | `VARCHAR` | yes |
| IntPointField | integer | `int64` | `int64` | `BIGINT` | yes |
| LongPointField | integer | `int64` | `int64` | `BIGINT` | yes |
| FloatPointField | float | `float64, from the text of the float32` | `float64` | `DOUBLE` | yes |
| DoublePointField | float | `float64` | `float64` | `DOUBLE` | yes |
| BoolField | boolean | `bool, from the text "true" or "false", by the field type in metadata.COLUMNS (D166)` | `bool` | `VARCHAR` | yes |
| DatePointField | timestamp | `time.Time, in UTC, with milliseconds` | `time.Time` | `TIMESTAMP` | yes |
| BinaryField | binary | `[]byte, from the base64 text, by the field type in metadata.COLUMNS (D166)` | `[]uint8` | `VARCHAR` | yes |
| UUIDField | uuid | `uuid.UUID, from its text, by the field type in metadata.COLUMNS (D166)` | `uuid.UUID` | `VARCHAR` | yes |
| LatLonPointSpatialField | string | `string, such as "45.5,-122.6"` | `string` | `VARCHAR` | yes |
| SpatialRecursivePrefixTreeFieldType | string | `string, the WKT text` | `string` | `VARCHAR` | yes |
| BBoxField | string | `string, such as "ENVELOPE(-10, 20, 15, 10)"` | `string` | `VARCHAR` | yes |
| PointType | string | `string, such as "1.5,2.5"` | `string` | `VARCHAR` | yes |
| multi-valued field | array | `[]any, of the JSON values of its elements` | `[]interface {}` | `ANY` | yes |
| aggregate | number | `int64, float64, or *apd.Decimal for an integer too large for int64` | `interface {}` | `` | yes |
| score | float | `float64` | `float64` | `DOUBLE` | yes |
<!-- /dbimp:types -->

These facts were recorded on each release, from documents with a field of
each type (recorded: "every type", unless a line names another request):

- A row is JSON, and names no type. `metadata.COLUMNS` names a SQL type for
  each field of a collection, to `admin` and to `dbmeta_user`: `VARCHAR`
  (12) for a string, a text, a boolean, a binary, a UUID and each spatial
  field, `BIGINT` (-5) for an int and a long, `DOUBLE` (8) for a float, a
  double and `score`, `TIMESTAMP` (93) for a date, and `ANY` (2000) for a
  multi-valued field and `_text_` (recorded: "the types of the columns" and
  "lead: the types of the spatial fields"). A vector names `DOUBLE`, and a
  date range names `TIMESTAMP` (recorded: "the types of the columns").
- An `IntPointField` and a `LongPointField` keep every digit, to
  -9223372036854775808 and 9223372036854775807.
- A `FloatPointField` writes the text of its float32: 3.4028235E38 and 0.1. A
  `DoublePointField` writes 1.7976931348623157E308, and 2.0 with its
  fraction.
- A document whose double holds `Infinity`, or whose float holds `NaN`, fails
  the query before any row, with `class java.lang.String cannot be cast to
  class java.lang.Double` (recorded: "lead: infinities").
- A single-valued `BoolField` is the string `"true"` or `"false"`, and a
  multi-valued one is an array of JSON booleans. A filter `n_b = true` works
  (recorded: "a filter on a boolean").
- A `DatePointField` is ISO 8601 text in UTC with milliseconds, such as
  `"2026-10-01T12:34:56.123Z"`. Solr cut the nanoseconds of the input to
  milliseconds. `0001-01-01T00:00:00Z` and `9999-12-31T23:59:59.999Z` read
  back as they were written. A filter compares a date with its text
  (recorded: "a filter on a date").
- A `BinaryField` is base64 text. A `UUIDField` is its text. A
  `LatLonPointSpatialField` is `"lat,lon"`. A
  `SpatialRecursivePrefixTreeFieldType` is WKT, such as `"POINT(-122.6
  45.5)"`, a `BBoxField` is `"ENVELOPE(-10, 20, 15, 10)"`, and a `PointType`
  is `"1.5,2.5"` (recorded: "lead: spatial fields", on 10.0.0 only).
- A multi-valued field is a JSON array of its values. Through the select
  handler, the values keep the order of the document. Through the export
  handler, they come sorted, such as `[-1,9223372036854775807]` for a
  document that held `[9223372036854775807,-1]` (recorded: "every type with
  docValues, with no limit").
- The update handler drops an empty string, an empty array, and an array
  that holds only `""`, so each of them reads as null.
- Text outside ASCII arrives as UTF-8, and a character outside the Basic
  Multilingual Plane as a pair of `\u` escapes, such as `🙂`.
- An aggregate names no type. `count(*)` and `sum` of integers are JSON
  integers, and `avg` of the integers 2147483647 and -2147483648 is `0`, an
  integer, where the mean is -0.5 (recorded: "the average of integers").
- `score` is a JSON number, such as 0.13076457 (recorded: "feature: score").
- A `DenseVectorField` fails the query when a row holds a vector, with
  `class java.util.ArrayList cannot be cast to class java.lang.Double`, after
  the rows before it (recorded: "a vector"). A `DateRangeField` fails the
  same way with `Text '[2026-01-01 TO 2026-12-31]' could not be parsed at
  index 0` (recorded: "a date range"). The select handler returns both
  (recorded: "every type through the select handler").
- An `EnumFieldType` and a `CurrencyFieldType` need a configuration file
  that `_default` does not hold. The Schema API refused the first with HTTP
  400, and failed the second with HTTP 500 and `Unable to reload core`,
  which also broke the collection (recorded: "add an enum type with no
  configuration file" and "add a currency type with no configuration file").
- An expression in the projection, such as `n_i * 2` or a `CAST`, fails with
  `Cannot invoke "String.contains(java.lang.CharSequence)" because "fname" is
  null`, so no column holds a computed value (recorded: "an expression in the
  projection"). `SELECT 1 AS one` fails because `one` is a word of the
  grammar (recorded: "a literal in the projection").

These facts come from the sources, and are not measured:

- The SQL module maps the field types `string` to `String`, `int`, `long`,
  `pint` and `plong` to `Long`, `float`, `double`, `pfloat` and `pdouble` to
  `Double`, `pdate` to `Date`, and a multi-valued field to `ANY`. Any other
  type takes its Java class from the class of the field type, and falls back
  to `String` (the SQL module).
- The JDBC driver takes the type of a column from the Java class of its value
  in the first row, and calls a null column a `String` (the JDBC driver).

## Parameters

These facts were recorded on each release:

- `?` fails with HTTP 500 and an HTML page, `java.lang.AssertionError:
  unsupported predicate expression: =(CAST($4):VARCHAR, CAST(?0):VARCHAR)`
  (recorded: "a placeholder").
- `:id` is a syntax error (recorded: "a named parameter").
- A quote in a literal is written twice, as `'it''s'` (recorded: "a quote in
  a literal").

These facts come from the sources, and are not measured:

- The server binds no parameters. The JDBC driver has a `PreparedStatement`
  whose `setString`, `setInt` and every other setter do nothing, so it sends
  the text with its `?` (the JDBC driver).

## Transactions

These facts were recorded on each release:

- `BEGIN` and `COMMIT` are syntax errors (recorded: "begin" and "commit").
- `POST /solr/dbimp/update?rollback=true` fails with HTTP 500, `Rollback is
  currently not supported in SolrCloud mode. (SOLR-4895)` (recorded: "lead:
  roll back the update handler").
- A write of the update handler with `commit=true` is visible to the next
  statement (recorded: "crud: insert" and "crud: select").

These facts come from the sources, and are not measured:

- The JDBC driver has `commit` and `rollback` that do nothing (the JDBC
  driver).
- A document can carry `_version_` for a check of optimistic concurrency on
  one document (Gemini).

## Errors

These facts were recorded on each release:

- An error of a statement answers HTTP 200, with the EOF tuple that holds
  `EXCEPTION`, such as `Failed to execute sqlQuery 'SELEC id FROM dbimp'
  against JDBC connection 'jdbc:calcitesolr:'. Caused by: ...` (recorded: "a
  syntax error", "a table that does not exist" and "a column that does not
  exist").
- An error can come after some rows. A date range in the third row gave two
  rows and then the EOF tuple with `EXCEPTION` (recorded: "an error after some
  rows"). A `GROUP BY` of an integer in `map_reduce` mode gave the group of
  -2147483648 and then failed on the group of NULL, `class java.lang.String
  cannot be cast to class java.lang.Long` (recorded: "an error after some rows
  of a group").
- A `?` answers HTTP 500 with an HTML page (recorded: "a placeholder").
- A wrong password answers HTTP 401, and a request that the role does not
  allow answers HTTP 403, each with an HTML page (recorded: "a wrong password"
  and "crud: insert" as `dbmeta_user`).
- The Collections API and the Schema API answer an error as JSON with
  `responseHeader.status` and `error.msg`. On 9.x `error.metadata` is a list
  of names and values, and on 10.0.0 an object (recorded: "remove the
  collection of an earlier run").
- No answer was HTTP 429 or HTTP 503.

## Cancellation and timeouts

These facts were recorded on each release:

- A client that left a query after 1 ms did not stop the next statement,
  which answered at once (recorded: "a query after the client left").
- `canCancel=true` with `queryUUID` on `/sql` registers nothing that
  `/tasks/cancel` knows: the cancel answered `"cancellationResult": "not
  found"` after the query ended (recorded: "lead: a query that can be
  cancelled" and "lead: cancel a query by its id"). `/tasks/list` answered an
  empty list to both users (recorded: "the tasks that run").
- `timeAllowed=1` did not cut a result of 300 rows (recorded: "lead: a time
  limit").

These facts come from the sources, and are not measured:

- When the client leaves, the next write of the stream fails, and the
  handler stops the stream that it runs. Whether the workers of
  `map_reduce` stop is not known (Gemini). DeepSeek said that Solr does not
  stop a query when the client leaves. The data of the measurements was too
  small to show either.
- The JDBC driver throws `UnsupportedOperationException` from `cancel` (the
  JDBC driver).

## Statements

These facts were recorded on each release:

- Two statements in one request fail with a syntax error at the `;`, and so
  does one statement that ends with `;` (recorded: "two statements in one
  request" and "a statement that ends with a semicolon").
- Comments `--` and `/* */` work (recorded: "comments").
- A name can be quoted with backticks. A text in double quotes is a name, so
  `id = "1"` fails (recorded: "quoted names" and "a string in double
  quotes").
- `EXPLAIN PLAN FOR` gives one row with the column `PLAN` (recorded:
  "explain").
- `UNION`, `DISTINCT`, `HAVING` and an order by an aggregate work (recorded:
  "feature: union", "distinct values" and "feature: having and an order of an
  aggregate"). An order by `max(id)` of a string fails with a
  `ClassCastException` (recorded: "lead: distinct ordered by another column,
  with no limit").
- A `JOIN` works. A join of `dbimp` with itself on `id` gave the row of `1`
  (recorded: "lead: a join of a collection with itself", on 10.0.0 only). A
  join of `dbimp` with the empty collection `dbmeta` gives no rows and no
  error (recorded: "feature: join").
- `field = 'text'` on a `TextField` is a search of the text, `n_s = 'h*'` is a
  wildcard, and `n_ss = 'a'` matches a multi-valued field that holds `a`
  (recorded: "feature: full text search", "feature: a wildcard in equality"
  and "feature: a multi-valued field in a filter"). `_query_ = 'n_t:fox'` runs
  a query of Lucene, and `_query_ = 'n_t:fox AND n_i:[0 TO *]'` fails with
  `Cannot parse 'n_i:[0'` (recorded: "feature: a lucene query" and "feature: a
  lucene query with spaces"). `ARRAY_CONTAINS` is not a function (recorded:
  "feature: array contains").
- A streaming expression is `POST /solr/<collection>/stream` with the form
  field `expr`, and answers in the same form as `/sql`, to both users
  (recorded: "a streaming expression").

## Principals

These facts were recorded on each release, as `dbmeta_user`, who has the
role `search`:

- `dbmeta_user` runs every SQL statement on a collection that exists, with
  the same answer as `admin`, `metadata.COLUMNS` and `metadata.TABLES`
  included (recorded).
- `dbmeta_user` cannot write. The update handler answers HTTP 403 (recorded:
  "crud: insert", "crud: update" and "crud: delete").
- `dbmeta_user` cannot read the Schema API, the list of collections, or the
  version, which `/solr/admin/info/system` and `/api/node/system` give to
  `admin` as `lucene.solr-spec-version`, such as `10.0.0`. Each answers HTTP
  403 (recorded: "schema: the fields", "the list of collections", "the
  version" and "the version through the v2 API").
- `dbmeta_user` can read the luke handler, `/solr/<collection>/admin/luke`,
  which names each field and its type, and can run a streaming expression
  (recorded: "the luke handler" and "a streaming expression").
- SQL has no statement that gives the version (Gemini, not measured). So the
  ordinary user cannot learn the version.

## Flavors

- No other product answers `/sql` in this form, as far as the sources know.
  Gemini named Lucidworks Fusion and Alfresco Search Services, which bundle
  Solr. Neither is measured, and `dbrun` starts neither.
- The three releases differ in these answers only (recorded):
  - An error of the Collections API or the Schema API holds
    `error.metadata` as a list on 9.x and as an object on 10.0.0.
  - The path of a collection that does not exist answers 404 on 9.x and 405
    on 10.0.0, for `admin`.
  - `GET /solr` redirects with 302 on 9.x and 301 on 10.0.0.
- Solr also runs in standalone mode, where `/sql` fails with an error,
  because SQL needs SolrCloud (the SQL module, not measured).

## Interfaces

Step 10 writes this table from the code.

<!-- dbimp:interfaces -->
<!-- /dbimp:interfaces -->

## Faults

`usql` has no driver for Solr. These are faults of the JDBC driver, which a
driver here must not repeat (the JDBC driver):

- `commit` and `rollback` do nothing, so a caller believes that it has a
  transaction (D20).
- Each setter of `PreparedStatement` does nothing, so a statement with `?`
  reaches the server with its `?`, and fails with HTTP 500 (D34).
- It takes the type of a column from the Java class of its value in the
  first row, calls a column of nulls a `String`, and gives a `Long` the
  JDBC type `DOUBLE`.
- `setMaxRows` appends ` limit <n>` to the text of a statement that has no
  limit.
- `cancel` throws `UnsupportedOperationException`.

These are faults of the server that a caller of a driver meets:

- Two columns with one name give wrong values (Responses).
- A `GROUP BY` in `map_reduce` mode with an order by an aggregate and no
  `LIMIT` gives 100 groups, with no sign that it cut the result (Responses).
- A value that the SQL layer cannot convert, such as a vector, a date range
  or an infinity, fails the whole query, and a caller cannot read the other
  rows (Types).

## Second opinions

Step 5a asked Gemini and DeepSeek on 2026-10-01 for the operations, the
features and the types. The server settled each answer:

- Gemini said that SQL only reads, and that the update handler writes. The
  server agrees. DeepSeek said that `INSERT`, `UPDATE` and `DELETE` work in
  SQL. The server refuses each one (recorded: "crud: sql insert", "crud: sql
  update" and "crud: sql delete").
- Gemini said that a `BoolField` arrives as a JSON boolean, and DeepSeek
  agreed. A single-valued one arrives as a string, and a multi-valued one as
  booleans (recorded: "every type").
- Both said that a `DenseVectorField` arrives as an array of numbers, and
  that a `DateRangeField` and a `CurrencyFieldType` arrive as text. The
  first two fail the query (recorded: "a vector" and "a date range"). The
  Schema API refused the third without its configuration file.
- Both said that a `LongPointField` keeps every digit. The server agrees.
- Gemini said that an alias of a collection can stand for a view. A
  statement on an alias works (recorded: "a statement on an alias").

Step 7 asked both models on 2026-10-02 what step 6 did not find. Each lead
was sent to the server:

- Both said that the server binds no parameters. The server refuses `?` and
  `:id` (recorded).
- Both said that there are no transactions. Gemini named
  `update?rollback=true`, which rolls back every change since the last
  commit. SolrCloud refuses it (recorded: "lead: roll back the update
  handler").
- Both said that there is no cursor, and that a `GROUP BY` in `facet` mode
  has a default limit of 100 when it is ordered by an aggregate. In `facet`
  mode the server gave all 302 groups. In `map_reduce` mode it gave 100 of
  301 (recorded: "lead: a group ordered by its count, with no limit" and
  "lead: a group ordered by its count, map reduce").
- Gemini named the Schema API, the luke handler and `metadata.COLUMNS` for
  the types. DeepSeek named `metadata.COLUMNS`. The ordinary user can read
  the luke handler and `metadata.COLUMNS`, and not the Schema API (recorded).
- Both said that `canCancel` and `queryUUID` do not reach `/sql`, and that
  `timeAllowed` is a limit and not a cancel. `/tasks/cancel` did not find
  the query, and `timeAllowed=1` cut nothing (recorded: "lead: cancel a query
  by its id" and "lead: a time limit").
- Gemini said that `numWorkers` and `workerCollection` change how
  `map_reduce` runs. `numWorkers=2` fails on one node (recorded: "lead: two
  workers").
- DeepSeek named the spatial types RPT and BBox. Each arrives as its text, and
  the SQL layer names each `VARCHAR` (recorded: "lead: spatial fields" and
  "lead: the types of the spatial fields").
- Neither model named a flavor that `dbrun` can start.

Step 8a asked both models on 2026-10-02 to review the type table against
[TYPES.md](TYPES.md) and D135, with the values that the server sent:

- Both agreed with each row. Both said that a `BoolField`, a `BinaryField`, a
  `UUIDField` and each spatial type give a `string`, because the SQL layer
  names each one `VARCHAR`, and D135 gives a column named as a string a
  string.
- Both said that `metadata.COLUMNS` is the only source of the type that the
  database names, and that an aggregate, which it does not name, takes its
  type from its JSON token.
- DeepSeek said that an empty array arrives as null, so a multi-valued field
  gives nil and never an empty `[]any`. The server agrees (recorded: "every
  type").
- DeepSeek said that the types that fail on the server, the vector, the date
  range, the enum and the currency, get no row until the server returns a
  value.

## Open questions

Each of these waits for Ken. Step 9 decides the rest with them.

1. SQL in Solr cannot write, and DRIVER.md says to stop at a server that
   refuses insert, update or delete. Is a driver that only reads still a
   target? If it is, does `ExecContext` refuse every statement, or does the
   driver send writes to the update handler, which is not SQL?
2. The result names no type. The table above takes the type of each column
   from `metadata.COLUMNS`, matched through `includeMetadata`, which costs a
   second query, or a cache for each collection. The other choice types each
   value by its JSON token only, so a date is text and a `BIGINT` is a
   number. Which one?
3. A `BoolField`, a `BinaryField` and a `UUIDField` give a `string`, because
   the SQL layer names each one `VARCHAR`, though the schema of Solr names
   them boolean, binary and UUID. Is that the mapping?
4. A `GROUP BY` in `map_reduce` mode can cut its result at 100 groups with no
   sign (D21). Does the driver refuse `aggregationMode=map_reduce`, or
   document the cut?
5. With no rows and no `includeMetadata`, the result names no column (D18).
   Does the driver always send `includeMetadata=true`?
6. Two columns with one name give wrong values. Does the driver refuse a
   result whose metadata names one alias twice?
7. The ordinary user cannot learn the version. Is that acceptable for the
   version statement of step 16?
