# D199. Answers after the releases

Status: Decided.

These are the answers of Ken on 2026-10-11, after the releases `v0.17.0` to `v0.19.0`.

1. The rqlite driver keeps matching every HTTP 401 for `ErrAuthentication`
   (D197), because the server sends the same empty 401 for a wrong password and for
   a missing permission, and RQLITE.md says so. A user who lacks the permission can
   be told that the credential was refused.
2. `WithParameter` of the Databricks driver and of the BigQuery driver sets a member
   of the request body, and refuses the members that change how the driver binds or
   reads a value. For Databricks they are `warehouse_id`, `statement`, `parameters`,
   `wait_timeout`, `on_wait_timeout`, `row_limit`, `byte_limit`, `disposition` and
   `format`. For BigQuery they are `query`, `queryParameters`, `parameterMode`,
   `useLegacySql`, `formatOptions` and `queryResultsFormat`.
3. `ColumnTypeNullable` of the Databricks driver returns `true` and `false`, which says
   that the column can be NULL and that the driver does not know, because the server
   sends no nullability. The rows of the Databricks driver keep no context, so rule 4
   of AGENTS.md does not name it.
4. `WithReadonly(true)` of the Cosmos DB driver runs the statement, because the driver
   reads only. `WithTimeout` with a positive value returns `dbimp.ErrNotSupported`,
   because the REST API has no such setting.
5. Ken accepted items 7 and 8 of D195: the BigQuery driver sends no Authorization
   header for a DSN with an endpoint and no key file or token, and the tests of
   Elasticsearch, OpenSearch and Solr accept both answers of the ordinary user for the
   version.
6. The Lint job pins `golangci-lint` v2.14.0, and the Go of the job stays the version of
   `go.mod`. The two move together.
