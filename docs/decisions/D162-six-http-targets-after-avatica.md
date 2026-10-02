# D162. Six HTTP targets after Avatica

Status: Decided.

Ken decided this on 2026-10-01, at step 1 of each driver. It amends D73 and
D74, which set the order up to Avatica, and the example of Elasticsearch and
OpenSearch under Flavors in [DRIVER.md](../DRIVER.md).

- The next targets are Apache Druid, Apache Drill, Apache Solr,
  Elasticsearch, OpenSearch and Amazon DynamoDB, in that order, after
  Avatica. Each one takes queries over HTTP, and `dbrun` starts it.
- Elasticsearch and OpenSearch get a driver each, `elasticsearch` and
  `opensearch`. OpenSearch forked from Elasticsearch 7.10, and since then its
  SQL endpoint and that of Elasticsearch differ, by their documents (not
  measured). Code that the two share goes in the root package (D4).
- The driver `dynamodb` serves DynamoDB Local and ScyllaDB Alternator, which
  speaks the API of DynamoDB, as flavors. `usql` reaches DynamoDB through
  `btnguyen2k/godynamo` now, so this driver can replace it (D24).
- The six go through step 9 of [DRIVER.md](../DRIVER.md) together. Ken
  reviews their type tables and their decisions at once, and then each
  driver is written from step 10.
