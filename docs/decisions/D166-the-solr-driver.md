# D166. The Solr driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Apache Solr
(W27). Each fact is in [SOLR.md](../SOLR.md). D163 decides that the driver is
read only, because the SQL of Solr refuses `INSERT`, `UPDATE` and `DELETE`.

1. The package and the name that it registers are `solr` (D26 and D28).
2. The DSN is `solr://user:password@host:8983/<collection>`. The path names
   the collection whose `/sql` handler takes the statement, and `FROM` names
   the table. The keys are `tls` (false by default) and `mode` (`facet` by
   default, sent as `aggregationMode`). The driver refuses `map_reduce`,
   because it cut a `GROUP BY` at 100 groups with no sign. Any other key is
   refused.
3. The Go types are the type table of SOLR.md. The answer names no type, so
   the driver reads the type of each column from `metadata.COLUMNS` once for
   each collection, and keeps it for the connection. Ken decided on
   2026-10-02 that the driver reads the field type there too: a `BoolField`
   is a `bool`, a `BinaryField` a `[]byte` from base64, and a `UUIDField` a
   `uuid.UUID`, though the SQL of Solr names each one `VARCHAR`.
4. NULL and a missing value are one value, nil.
5. Solr binds no argument, so the driver writes each one as a literal with
   the escaper of the root package, which doubles a quote (D34).
6. `BeginTx` returns `dbimp.ErrNotSupported`, because Solr has no
   transactions.
7. Each request sends `includeMetadata=true`, so that the first tuple names
   the columns, also for a result with no rows (D18). The driver reads each
   tuple one token at a time, to the tuple `EOF`. An `EXCEPTION` in it is
   the error, and after some rows it wraps `dbimp.ErrIncomplete` (D21 and
   D107). A statement whose columns share a name is refused, because the
   server gives wrong values for them.
8. The driver closes the request when the context ends. Solr has no way to
   stop a statement of `/sql`. Whether it stops one when the client leaves
   is not measured.
9. The driver follows no redirect, and sends the credentials to the host of
   the DSN only.
10. The driver serves no flavor.
11. The driver uses JSON only.

The ordinary user cannot read the version, which waits for `dbmeta`.
