# D29. The module imports no other xo package

Status: Decided.

Ken decided this on 2026-09-27. This module never imports `dburl`, `dbmeta`,
`usql` or `dbtpl`. It reads information from them when it needs a fact, such
as a scheme in `dburl/scheme.go` or a release in `dbmeta/container/`. It runs
`dbrun` from a checkout of `dbmeta` as a tool, to start the servers for its
tests. None of them is in `go.mod`, and `depguard` refuses each one.
