# D57. The SurrealDB driver exports Version

Status: Decided.

Ken decided this on 2026-09-27, for step 16 of [DRIVER.md](../DRIVER.md). No
statement of SurrealQL returns the version, and only the RPC method
`version` and `GET /version` do, for both principals (measured on 2.7.0 and
3.3.0). The `Version` function of a driver of `usql` gets only the methods
of a query. So the driver exports `Version(ctx, dc)`, which calls the RPC
method `version` on a connection of the driver. A caller hands it the
connection inside `sql.Conn.Raw`, and `usql` does that from its `Version`
function. The driver never rewrites the text of a statement to serve it.
