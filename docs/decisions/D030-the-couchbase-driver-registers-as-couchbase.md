# D30. The Couchbase driver registers as couchbase

Status: Decided.

Ken decided this on 2026-09-27, and it answers Q10. The Couchbase driver
registers the one name `couchbase` with `database/sql`, the same as its
package (D26 and D28). It never registers `n1ql`.

`dburl` names the scheme `n1ql` today, with `couchbase` as an alias, because
`go_n1ql` registers `n1ql`. At the move of step 16 of
[DRIVER.md](../DRIVER.md), the `dburl` session makes `couchbase` the `Driver`
name of the scheme and keeps `n1ql` as an alias. The change waits for the
move, because `usql` opens `go_n1ql` by the name `n1ql` until then.

The aliases of the scheme belong to `dburl`, and this repository does not
choose them (D35). The `dburl` session reported that the `Dialect` of the
scheme changes from `n1ql` to `couchbase` at the rename, because the
`Dialect` of a scheme names its own `Driver` (dburl D19). A consumer that
matches on the `Dialect` `n1ql`, in `usql`, `dbtpl` or `dbmeta`, changes in
the same release.

The rule for every driver follows from D26 and D28: the registered name is
the name of the package, which is the name of the database. Where `dburl`
names a scheme differently, the `dburl` session renames it at the move.

Note of 2026-09-29: the move is done. dburl `v0.33.0` made `couchbase` the
name of the scheme, with the aliases `n1ql` and `n1`, for `v0.1.0` of the
driver (dburl D25). dburl `v0.36.0` calls that field `Name` (dburl D37).
