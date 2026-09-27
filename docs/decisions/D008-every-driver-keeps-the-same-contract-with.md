# D8. Every driver keeps the same contract with database/sql

Status: Decided.

Each driver does all of these:

- It implements `driver.DriverContext` and `driver.Connector`, and the
  context forms of every interface it implements, such as
  `driver.QueryerContext` and `driver.ExecerContext`. It stops work when the
  context ends.
- It returns a NULL as nil and never as a zero value. `go-cql-driver`
  returned an empty string for a CQL NULL, which hid faults for months, until
  the move to `xo/cql` exposed them (dbmeta D62 and dbmeta D93).
- It keeps the column order of the statement, returns decoded Go values, and
  never wraps one column in an object. `go_n1ql` sorted the columns by name,
  returned JSON text, and wrapped a row of one column as `{"v": ...}`, and
  each of those ruled out a `dbmeta` model (dbmeta D94).
- It sends a statement to the server as the caller wrote it, and does not
  split it. `vertica-sql-go` splits at every `;` and so cannot create a SQL
  function (dbmeta D88). If a product needs a split, the rule for it is
  written in a decision first.
- It returns every error to the caller, wrapped with `%w`. It returns
  `driver.ErrBadConn` only when the statement did not reach the server,
  because `database/sql` then runs the statement again, and a second run of a
  write is a second write.

`dbmeta` needs nothing beyond a correct driver. It takes any value with a
`QueryContext` method and runs every statement itself. `dbmeta` hard rule 10
requires its test module to use the same package that `usql` uses for each
database, so a driver lands in `usql` and in `dbmeta` together.
