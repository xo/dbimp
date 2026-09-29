# D96. The first InfluxQL column is measurement

Status: Amends D81.

Ken accepted this on 2026-09-29, after the tests of `usql` against `v0.4.0`.
D81 named the first column of each InfluxQL result set `name`, as the `csv`
output of the `influx` command does. Many series have a column `name` of
their own, so a set can have two columns of that name. `SHOW DATABASES` gave
`name | name`, and a client that finds a column by its name cannot tell the
two apart.

The first column is now `measurement`. It holds the name of the series, as
before. The rest of D81 does not change: the keys of `tags` come next, then
the columns of the series. `SHOW DATABASES` gives `measurement | name`.

A tag or a column of the series that is itself called `measurement` still
gives two columns of that name. InfluxDB 1.13.1 took a tag `measurement`
and returned it in `columns` (measured). The driver does not rename a
column of the server.

This changes a released driver. A caller of `v0.4.0` that reads the first
column by the name `name` reads `measurement` from the next release.
