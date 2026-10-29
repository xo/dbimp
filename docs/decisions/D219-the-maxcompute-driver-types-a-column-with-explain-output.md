# D219. The MaxCompute driver types a column with EXPLAIN OUTPUT

Status: Decided.

The CSV result of MaxCompute carries no type, and no call of the REST API returns a schema. The
statement `EXPLAIN OUTPUT <query>;` answers the name and the exact type of each column as text,
with no table and no privilege (recorded: "lead: explain output of every type"). Ken decided on
2026-10-29 that the driver uses it, in place of the quoting rule that he had chosen before step 7.
Decided:

1. A query of `QueryContext` sends `EXPLAIN OUTPUT <query>;` first, and reads the type of each
   column from its text, by position. The names come from the header of the CSV, because the two
   differ for a repeated name and an unnamed column ("lead: explain output with a repeated name
   and no name").
2. Each cell is decoded by the type of its column, with the type table of `docs/MAXCOMPUTE.md`,
   which Ken reviewed on 2026-10-29 (D135 and D137). A `DATETIME` and a `TIMESTAMP` is a
   `time.Time` in the time zone of the project: the setting `odps.sql.timezone` if the caller gives
   it, and the property of that name from `GET /api/projects/<project>` otherwise. An `ARRAY`, a
   `MAP` and a `STRUCT` is the text of the server.
3. A NULL is the cell `"\N"`, and it reaches `database/sql` as nil (D18). A string of a backslash
   and an N has another form, so the two never mix.
4. A statement that is not a query, which is a statement of DDL or DML or a `SHOW`, is not sent
   to `EXPLAIN OUTPUT`. `ExecContext` never sends it. `QueryContext` sends it for every statement,
   and when its answer is an error, the driver sends the statement itself and returns the result
   as one column named `text` with one row, which holds the whole text (decided by Ken on
   2026-10-29).
5. A result of exactly 10,000 rows is returned as it is, with no error (D220).

See `docs/MAXCOMPUTE.md`, section "Types".
