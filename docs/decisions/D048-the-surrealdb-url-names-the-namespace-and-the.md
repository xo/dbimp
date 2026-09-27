# D48. The SurrealDB URL names the namespace and the database in its path

Status: Decided.

Ken decided this on 2026-09-27. It decides the form of item 3 of step 9 for
SurrealDB. The URL is
`surrealdb://user:pass@host:port/<namespace>/<database>`, such as
`surrealdb://root:pw@127.0.0.1:8000/dbmeta/dbmeta`. Every request of
SurrealDB names a namespace and a database, so the path must hold exactly
two segments, and a URL with fewer or more is refused. A segment is decoded
by the rules of `net/url`, so a name with a `/` is written `%2F`.

The default port is 8000. The key `tls=true` makes the driver speak HTTPS on
the same port. Whether SurrealDB serves TLS on the port that it binds is not
measured, and step 6 measures it if `dbrun` can start it with TLS.
The other query keys wait for the measurements of step 6, and each one is a
decision of its own.
