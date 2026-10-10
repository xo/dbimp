# D198. Spanner database role and DDL batches

Status: Decided.

`dbmeta` measured the Spanner driver on the hosted service on 2026-10-11 and asked
for two things. Ken decided on 2026-10-11 to add both in the next release (W43).

1. The DSN has the key `database_role`. When it is set, the session that the driver
   makes sends it as the database role of the session (`creatorRole`), so that
   fine-grained access control applies. The default is no role. The key is also an
   option for one statement, as D109 asks for every key that can change.
2. A DDL batch is one `Exec` whose text has several DDL statements separated by
   semicolons. When every statement is DDL, the driver sends them in one
   `updateDatabaseDdl` request, as the array `statements`, and it polls the one
   operation. This follows D191 item 8 for a single statement. A text that mixes DDL
   with DML or a query is refused with an error that says so, and D191 still refuses
   several statements of DML and queries. The statements and the semicolons are the
   caller's, and a trailing semicolon is cut as before.
3. The driver registers the name `spanner`, as `go-sql-spanner` does, so one binary
   cannot import both (D28 and D30). `dbmeta` needs Spanner Omni, which only
   `go-sql-spanner` reaches over gRPC, so it keeps that driver in a binary of its
   own. The driver here does not register another name.
