# D9. A test that needs a server reads a DSN and skips without one

Status: Decided.

A unit test needs no server. A test that needs one reads the DSN from an
environment variable named for the driver in upper case, followed by `_DSN`,
such as `CQL_DSN` in `cql`. It calls `t.Skip` when the variable is empty. The
name of such a test holds the word `Integration`, so that
`go test -run Integration` selects it. There are no build tags for this and
no testcontainers.

`dbrun` in `dbmeta` starts every server, and nothing else does. `dbrun dsn`
prints the DSN that `dbmeta` gives a driver. A database that `dbrun` does not
know gets its entry in `dbmeta/container/` first, which is work for the
`dbmeta` session or for Ken. The CI workflow checks out a pinned commit of
`dbmeta` and runs `dbrun`, as in `cql` D20 and `n1ql` D28. A new pin is a
commit in this repository.

A driver is tested as the administrator and as an ordinary user. The parity
tests in `dbmeta` found queries that the server refused to an ordinary user,
and nobody had recorded them (dbmeta D61).
