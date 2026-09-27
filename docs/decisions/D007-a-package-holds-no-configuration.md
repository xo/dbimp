# D7. A package holds no configuration

Status: Decided.

A driver keeps no setting in a package level variable. A setting arrives in
the DSN, or in a `driver.Connector` that the caller builds and passes to
`sql.OpenDB`. A driver logs nothing.

One `usql` driver borrowed the configuration of another driver and shipped a
fault to users. `go_n1ql` keeps its credentials in package variables, and
that is the pattern not to copy. This follows `dbmeta` hard rule 6 and
`cql` D8.
