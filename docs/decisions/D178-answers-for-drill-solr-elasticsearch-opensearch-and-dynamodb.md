# D178. Answers for Drill, Solr, Elasticsearch, OpenSearch and DynamoDB

Status: Decided.

These are the answers to open questions of the drivers for Drill, Solr,
Elasticsearch, OpenSearch and DynamoDB (W26 to W30). Ken decided them on
2026-10-07.

1. Drill keeps the session cookie, as D165 item 10 says. The settings
   `autoLimit`, `defaultSchema` and `options` then stay in the session, so a
   later statement on the same connection with no setting of its own still
   gets the limit, and rows are lost with no sign. DRILL.md says so, and says
   that a caller clears the limit with `ALTER SESSION RESET ALL`. It is
   question 9 of DRILL.md.
2. The rows of an Elasticsearch query store a context, because the request for
   each next page needs it. Hard rule 4 of AGENTS.md names them, as it names
   the rows of a Trino and a Databend query. It is question 6 of
   ELASTICSEARCH.md. The rows of an OpenSearch query keep the context for the
   same reason, and rule 4 names them too. It is question 11 of OPENSEARCH.md.
3. `Rows.Close` of an Elasticsearch query before the end reads the rest of the
   current page, at most 256 KiB and 5 seconds, to learn the cursor, and then
   sends `/_sql/close`. This differs from D36 for this driver, because D167
   item 7 asks for the close of the cursor. It is question 7 of
   ELASTICSEARCH.md.
4. The Solr driver reads the SQL type of a column from `metadata.COLUMNS` and
   the class of its field type from the luke handler, once for each table and
   connection, because `metadata.COLUMNS` names `VARCHAR` for a boolean, a
   binary and a UUID. A `BoolField` is a `bool`, a `BinaryField` is a `[]byte`
   and a `UUIDField` is a `uuid.UUID`. This amends item 3 of D166. It is
   question 8 of SOLR.md.
5. The OpenSearch driver returns the HTTP 403 that the ordinary user of 2.19.6
   gets for a plain `SELECT`, because the page size opens a cursor that needs
   the right to search every index. It does not send the statement again, and
   it always sends a page size to a plain `SELECT`. OPENSEARCH.md says that a
   caller turns paging off with `WithParameter("fetch_size", 0)`, and that the
   result is then cut at 10000 rows with no sign. It is question 13 of
   OPENSEARCH.md.
6. The OpenSearch driver closes the request and each cursor that it knows when
   the context ends or the caller closes the rows early, as the Elasticsearch
   driver does. It does not call `_tasks/_cancel`. OPENSEARCH.md says that the
   server runs a statement to its end after the client leaves, and that a
   cursor of a page that the context cut stays until its `keep_alive` of one
   minute ends. These are questions 10 and 14 of OPENSEARCH.md.
7. The step 6 recordings of OpenSearch are made again on 3.9.0, because
   `dbmeta` no longer starts 3.8.0 (question 17 of OPENSEARCH.md).
8. The rows of a DynamoDB query keep the context for the `NextToken` of each
   page after the first, so rule 4 names them too. It is a question of
   DYNAMODB.md.
9. The DynamoDB driver keeps the exported type `dynamodb.Set`, a `[]any` that a
   caller binds as a set. Its first element names the kind: `SS` for a string,
   `NS` for a number and `BS` for a `[]byte`. A set that the driver reads is a
   `[]any`, which is a list when a caller writes it back. Ken decided on
   2026-10-07 to keep it for now, and it can be removed later, as the `Row`
   type of Trino was.
10. The keys of the DynamoDB DSN are `tls`, `region` and `token`. The key
    `token` holds the session token of temporary credentials, and it is empty
    by default. This amends item 2 of D169. Ken decided on 2026-10-07 to add
    the key `token`.
11. The OpenSearch driver serves no flavor and sends no request for the
    release. This amends item 10 of D168, which said that the release comes
    from `GET /` or from a header.
12. The Solr driver accepts a DSN with no path, and a statement then needs
    `WithDatabase`. `WithTimeout` above zero fails with `dbimp.ErrNotSupported`,
    because `timeAllowed` cut nothing. An equality on a `StrField` treats `*`
    and `?` as wildcards, and the driver writes the literal of the caller as
    given. SOLR.md names the wildcards as a fault of the server. These are
    questions 9, 11 and 13 of SOLR.md.
13. The Elasticsearch driver fails `WithDatabase` with `dbimp.ErrNotSupported`
    and has the option `WithCatalog`, which sets the catalog. `WithParameter`
    applies only to the first request. A type that the driver does not know is
    the decoded JSON value. These are questions 10 and 11 of ELASTICSEARCH.md.
14. A driver whose server does not tell the count of the rows that a statement
    changed returns an error that wraps `dbimp.ErrNotSupported` from
    `RowsAffected`, with the reason. It returns the count whenever the server
    sends one, as the Trino driver does with `updateCount`. It never returns 0
    or -1 in place of the error. Ken decided this on 2026-10-07. Every driver
    here follows it already.
15. Ken confirmed on 2026-10-07 the choices that the agents made where a
    decision named none:
    - Trino: `QueryContext` waits for the first row, or for the end of an
      answer with none, as the official client does (question 12 of TRINO.md).
    - ClickHouse: `WithReadonly(true)` sends `readonly=1`, `Ping` runs `SELECT
      1` in place of `GET /ping`, and the transport lets go of an idle
      connection after 5 seconds (questions 19 and 22 of CLICKHOUSE.md).
    - Drill: the driver reads the profile of a failed query again for up to 2
      seconds, sends the cancel when the caller closes the rows early, and takes
      the options that the REST interface allows. `Ping` runs a `SELECT`.
      `RowsAffected` is the count of `CREATE TABLE AS`, and a trailing semicolon
      is not cut (questions 10 to 15 of DRILL.md).
    - OpenSearch: the word-scan rule for a plain `SELECT`, a failed row for a
      value that does not fit its type, and `WithTimeout` that fails with
      `dbimp.ErrNotSupported` (questions 12, 15 and 16 of OPENSEARCH.md).
    - Solr: the scan of `FROM` and `JOIN` for the tables of a statement, and
      the exported `ErrCut` (questions 12 and 14 of SOLR.md).
    - Elasticsearch: a `float` is read as a `float64`, and a day-time interval
      of more than about 292 years fails the row (questions 8 and 9 of
      ELASTICSEARCH.md).
    - DynamoDB: the column of `SELECT *` and of `RETURNING` is named `""`,
      `Error` keeps no `Item`, and a DSN with no user or no password is refused
      (questions 3 and 8 of DYNAMODB.md, and the DSN of D169).

16. Ken decided on 2026-10-07 that `dburl` writes the key `flavor` of a Trino
    or Presto URL from its scheme: `trino://` writes `flavor=trino` and
    `presto://` writes `flavor=presto`, unless the URL names a flavor. The
    driver is unchanged, because `flavor` is a key of its DSN already (D175),
    and so a `presto://` URL against a Trino server fails with the error of the
    server and not with a silent switch of flavor.
17. This amends item 15 for one case. On OpenSearch 2.19.6 the legacy
    `DESCRIBE TABLES` names every column `keyword` and sends numbers in
    `NUM_PREC_RADIX`, `NULLABLE` and `ORDINAL_POSITION`. The OpenSearch driver
    reads such a number as the value that arrived, an `int64` or a `float64`,
    and does not fail the row. The schema of that server is wrong, so the value
    is a decoded value and not text in place of one (hard rule 3).
    `ColumnTypeScanType` still names the type of the schema. OPENSEARCH.md
    says that this is a fault of 2.19.6. Ken decided this on 2026-10-08, at the
    request of `dbmeta`, which has no column source on 2.19.6 otherwise.
