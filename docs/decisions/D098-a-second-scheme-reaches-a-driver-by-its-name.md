# D98. A second scheme reaches a driver by its name

Status: Decided.

Ken decided on 2026-09-29 that `dburl` drops `Scheme.Override`, as this
session recommended to the `dburl` session. The change is in `dburl`, which
owns the schemes (D5). `dburl` released it in `v0.36.0` as dburl D37. This
decision records what it means for the drivers here.

Before `v0.36.0`, a scheme had two ways to open the driver of another
scheme:

- `Override` wrote the name of the other driver into `URL.Driver`, and the
  scheme took the dialect of the other scheme.
- `GoDriver` named the driver that `sql.Open` opened. `influxql` reached the
  package `influxdb` this way (D78).

From `v0.36.0`, both are gone, and each field of a URL of `dburl` has one
meaning. `URL.SchemeName` is the scheme, with no alias. `URL.Driver` is the
name to pass to `sql.Open`, which a package registers. `URL.Dialect` is the
product. That fits D28 and D30: a package registers one name, and every
other name is the business of `dburl`.

So a second scheme for a driver here takes this form:

- It has a `Name` and a `Dialect` of its own, because a flavor or a second
  dialect speaks its own language. A flavor of Avatica (D74), such as the
  Phoenix Query Server, and the scheme of Presto, if one driver serves
  Trino and Presto, are cases to come.
- Its generator writes the URL that the driver reads, whose scheme is the
  registered name (D35), with the keys that choose the flavor or the
  dialect, as `influxql` writes `influxdb://` with `sqlmode=disable`.
- Its generator returns the registered name, which `dburl` stores in
  `URL.Driver`.

A second name for the same product and the same dialect, such as `turso`
for `libsql` (D76) or `n1ql` for `couchbase`, stays an alias, and needs
neither.

No driver here depended on `Override`. The drivers do not change, because
they never read a URL of `dburl` (D29).

This session first wrote that `GoDriver` would stay. `dburl` then removed
it too, on 2026-09-29, and this text follows `v0.36.0`.
