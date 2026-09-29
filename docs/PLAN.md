# Plan

`dbimp` holds `database/sql` drivers for databases that have no idiomatic Go
driver, and for groups of databases that can share one implementation. The
first drivers speak HTTP (D14), and each driver is its own package, with the
code that they share in the root package (D4). `usql`, `dbtpl` and `dbmeta`
consume the drivers, and `dburl` hands each driver its DSN (D35). A driver
here can replace a driver that `usql` imports now, which reduces the
dependencies of `usql` (D24).

The drivers so far are Couchbase (D23), SurrealDB, Neo4j, InfluxDB and
ArangoDB. The targets and
their order are in [TARGETS.md](TARGETS.md), and every step of a new driver
is in [DRIVER.md](DRIVER.md).

Every decision is a file in [decisions/](decisions/README.md), and the index
is there (D72). Every work item is in [BACKLOG.md](BACKLOG.md). This file
holds the purpose of the project and the open questions for Ken.

## Open questions

Do not decide these yourself. Ask Ken.

### Q1. Which targets come after Couchbase? Answered by D73.

Couchbase is first (D23). Ken arranged the other targets after the first
driver was complete. The review in [TARGETS.md](TARGETS.md) named a start:
CrateDB, QuestDB, rqlite, libSQL, InfluxDB 3 and TDengine, by D17.

On 2026-09-27, Ken named SurrealDB as the second target. W8 is that work.
The same day, he named Neo4j as the third. W9 is that work. The same day,
he set the order after Neo4j, in D73, and added Avatica after libSQL, in
D74. "The order" in [TARGETS.md](TARGETS.md) holds the result.

### Q2. Which licence? Answered by D22.

### Q3. How does a result that is not a table become rows? Answered by D18.

### Q4. Does the Couchbase driver here replace xo/n1ql? Answered by D23.

### Q5. Is a target that already has a Go driver in usql in scope? Answered by D24.

### Q6. What Go type holds a decimal? Answered by D33.

### Q7. What does a driver do when the server binds no arguments? Answered by D34.

### Q8. How does rows.Close release a cursor on the server? Answered by D36.

### Q9. Is "tier" the right word? Answered by D32.

### Q10. Does the registered name follow the package name? Answered by D30.

### Q11. Which short aliases does the Couchbase scheme keep? Answered by D35.

### Q12. What does a result set with no rows give as its columns? Answered by tblfmt D31.

`tblfmt` settled this on the side of the display, by option 1 below. Its D31,
in commit `62d9721` and released in `v0.19.1`, makes a result set with no
columns valid for every encoder, and `EncodeAll` goes on to the next result
set. The output matches `psql` 18.6. The commit removes
`ErrResultSetHasNoColumns`, and `usql` requires `tblfmt` `v0.19.1` (read from
the checkouts of `tblfmt` and `usql` on 2026-09-28, and confirmed by the
`tblfmt` session). So D52 stands, and no driver of this repository changes
for it.

The aligned format at border 1 writes `--`, then the count, such as
`(0 rows)`, and the JSON encoder writes `[]` for no rows and `{}` for each
row. The question as it stood:

The `dbmeta` session found this through `usql` at commit `54c12a4`, on
3.3.0. For a statement that returns no rows, such as `DELETE author`, or
`SELECT * FROM author` on an empty table, SurrealDB sends an empty array and
no names. So by D52 the result set has no columns. `tblfmt` refuses a result
set with no columns with "result set has no columns", and `usql` prints that
error after the statement ran. The Couchbase driver gives no columns for an
empty `SELECT *` too, because its signature names no field (D42).

PostgreSQL has results with no columns too, and `psql` does not refuse
them. On PostgreSQL 17.11, `psql` 18.6 printed `--` and `(1 row)` for
`select;`, and `--` and `(0 rows)` for `select * from t`, where `t` has no
columns, and it exited with 0. `usql` at `54c12a4` gave
`error: pgx: result set has no columns` for `select;` (measured on
2026-09-27). So the fault is in `tblfmt`, and not only in the driver.

The options are these:

1. Keep D52. `tblfmt` writes a result set with no columns without an error,
   and goes on to the next result set.
2. Keep D52. `usql` prints the type of the statement, such as `DELETE`, when
   `tblfmt` gives that error, as it does now for `EXEC` on SQL Server. This
   loses the result sets after the empty one.
3. Replace D52 for an empty array. The driver gives one column with the name
   `""`, as rule 3 of D18 does, and no rows. `tblfmt` v0.16.0 then prints
   `(0 rows)` (measured), and only the SurrealDB driver changes.
