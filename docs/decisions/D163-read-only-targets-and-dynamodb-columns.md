# D163. Read-only targets, the caps of OpenSearch, and DynamoDB columns

Status: Decided.

Ken decided this on 2026-10-02, after step 8a of the six drivers of D162. It
amends D162, which made ScyllaDB Alternator a flavor of `dynamodb`.

- The drivers of Druid, Drill, Solr, Elasticsearch and OpenSearch are read
  only, as the driver of Pinot is. Each one sends what its SQL takes, and
  returns the refusal of the server for the rest. Druid runs `INSERT` and
  `REPLACE` only as tasks that end later, so its driver does not send them
  to the task API, and they fail too (DRUID.md, DRILL.md, SOLR.md,
  ELASTICSEARCH.md and OPENSEARCH.md, under Requests).
- OpenSearch gets a driver. The driver sends a page size for each plain
  `SELECT`, so that a result is not cut at 10000 rows. Its documentation
  names each cap that the server applies with no sign: 1000 groups on
  2.19.6 and 10000 groups on 3.8.0 for `GROUP BY` and `DISTINCT`, and the
  older engine that answers a `LIMIT` or a `GROUP BY` with a page size
  (OPENSEARCH.md, Responses). This is the exception to D21 for these caps.
- The driver `dynamodb` serves DynamoDB, and not Alternator. Alternator
  refuses every PartiQL operation with `UnknownOperationException`, so it
  fails S (DYNAMODB.md, Flavors).
- The columns of a DynamoDB result are the ones that the statement names,
  in its order. `SELECT *` names none, so its result has one column, which
  holds each item as a `map[string]any` (D18, and DYNAMODB.md, Responses).
