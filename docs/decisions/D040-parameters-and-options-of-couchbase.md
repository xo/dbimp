# D40. Parameters and options of Couchbase

Status: Amended by D43.

Ken accepted this on 2026-09-27.

The driver sends each argument to the server, which binds it (D34). A
positional argument goes in `args`, and a `sql.Named` argument goes in
`$name`. `?` and `$1` in a statement are both positional, and the driver
changes neither. A value is encoded with `encoding/json/v2`, so a map, a
slice and a struct are arguments as well as a scalar. The driver implements
`driver.NamedValueChecker` for that, and returns `driver.ErrSkip` for a
`driver.Valuer`.

An option of one query comes from the DSN, then from the context through
`WithOptions`, then from an argument of the type `Option`, in that order, as
cql D23 does. The options are the keys of D38 without `tls`, and `readonly`.
