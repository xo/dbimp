# D76. The names of the Databend, libSQL and CrateDB drivers

Status: Amended by D88.

Ken decided these on 2026-09-28. They decide items 1 and 2 of step 9 of
[DRIVER.md](../DRIVER.md) for three drivers, ahead of their turn in D73.
Each package registers its one name, and the scheme of its URL is that name
(D26, D28, D30 and D35).

- Databend: the package is `github.com/xo/dbimp/databend`, and its name is
  `databend`. It replaces `github.com/datafuselabs/databend-go` in `usql`
  and in `dburl` (D24). So the two drivers are never linked into one
  program, and the name `databend` does not clash. [DATABEND.md](../DATABEND.md)
  holds the faults of databend-go that it replaces.
- libSQL and Turso: they are one product with one driver. The package is
  `github.com/xo/dbimp/libsql`, and its name is `libsql`. `turso` is an alias
  in `dburl`, which owns the aliases (D5). [LIBSQL.md](../LIBSQL.md) holds
  what is known.
- CrateDB: the package is `github.com/xo/dbimp/cratedb`, and its name is
  `cratedb`. D88 withdrew this driver, because `usql` reaches CrateDB with
  `pgx`. `dburl` has the scheme `cratedb` on `pgx`, with the dialect
  `cratedb`, from dburl D30. [CRATEDB.md](../CRATEDB.md) holds what is known.

The URL of each, its keys and its default port are still decisions of step
9 of each driver.
