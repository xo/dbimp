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
