# D171. No DDL in the drivers, and a read only SQL layer later

Status: Decided.

Ken decided this on 2026-10-07, after the `dbmeta` session gave its view of
the idea of a small grammar for the databases that do DDL only through an
API.

- The drivers write no DDL. A statement such as `CREATE TABLE` goes to the
  server as it is, and the server answers it or refuses it (D163 stands).
  The fixtures of `dbmeta` make their tables with the native API in the test
  module of `dbmeta`, which can use `net/http`, and not with SQL.
- A small SQL layer in a driver can answer `SHOW TABLES`, `DESCRIBE <name>`
  and `SELECT version()` from the native API, for a database with no SQL
  catalog. It reads and writes nothing. Ken decides it for each driver, when
  `dbmeta` needs it for the Dialect of that database.
- That layer is a hand written parser in a package of `dbimp` that the
  drivers share. It is not a grammar of `transit`, because a few statements
  do not need a generated parser, and `transit` would be a new package of
  every driver (hard rule 6). Gemini and DeepSeek both said so on
  2026-10-07, and so did `dbmeta`.
- A statement that the layer does not know goes to the server as it is. An
  answer of the layer names only what the native API gave, with no default.

Which targets need the layer is not decided. By their documents, Druid and
Drill have `INFORMATION_SCHEMA`, Solr has `metadata.COLUMNS`, and
Elasticsearch and OpenSearch answer `SHOW TABLES` and `DESCRIBE` in SQL.
DynamoDB, Pinot and ArangoDB have no SQL catalog, so they are the candidates.
