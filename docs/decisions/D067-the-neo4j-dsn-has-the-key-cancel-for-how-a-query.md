# D67. The Neo4j DSN has the key cancel, for how a query stops

Status: Decided.

Ken accepted this on 2026-09-27, and decided that day that the driver does all
three, and that the caller chooses in the DSN. It decides items 9 and 10 of
step 9 for Neo4j. The server does not stop a query when the client
disconnects, on either release (measured). So when the context ends during a
request, the driver closes the body, returns the error of the context, and
then does what `cancel` says:

- `cancel=tag`, the default. Each statement starts with the comment
  `/* dbimp:<id> */`, where the id is random and fixed for the connection.
  The driver finds the statement with `SHOW TRANSACTIONS` by its
  `currentQuery`, and stops it with `TERMINATE TRANSACTION`. The ordinary
  user can do both, on both releases, and in an explicit transaction too
  (measured). A tag fixed for the connection keeps the plan cache of the
  server warm: a new text took 10 ms to plan, and the same text took 1 ms
  (measured on 2026.09.0).
- `cancel=metadata`. The driver sends `txMetadata` with a random id, and
  finds the transaction by its `metaData`, so the text of the statement is
  not changed (measured on 2026.09.0). 5.26.31 refuses `txMetadata` (measured).
  So the driver reads `neo4j_version` from `GET /` once for each connector,
  and uses the tag on a release older than 2026.04.
- `cancel=none`. The driver sends nothing more, and the query runs on until
  it ends.

The requests that stop a query use the context of the call without its end,
through `context.WithoutCancel`, with a limit of 5 seconds. Their error is
joined to the error of the context. `TERMINATE TRANSACTION` ends an explicit
transaction, so its next request returns the error of HTTP 404.

The driver follows no redirect, and sends the credentials only to the host of
the URL. The server sent no redirect in any measurement.
