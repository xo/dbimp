# D233. The package is hive and Impala is a flavor

Status: Decided on 2026-11-09.

Ken decided on 2026-11-09 that the package for W56 is `github.com/xo/dbimp/hive`, and that Impala is a
flavor of it. This decides items 1 and 2 of the open questions of [HIVE.md](../HIVE.md), and part of
items 1 and 11 of step 9 of [DRIVER.md](../DRIVER.md).

1. The package is `hive`, and it registers one name with `database/sql`, which is `hive`. It has no
   alias (D28 and D30). The scheme `hive2` of `dburl` is not a name here.
2. Impala is a flavor, in the way that Presto is a flavor of `trino` (D173). It has no package of its
   own. The scheme `impala` of `dburl` reaches the one driver by its registered name, `hive`, with its
   own `Dialect` (D98).
3. The driver tells the flavors apart from what the server says, and never from the DSN alone
   (DRIVER.md, Flavors). The way to ask the server is measured in step 6.
4. Each flavor has its own container entry, its own measurements and its own verdicts in
   `features.json`. A difference goes in the section Flavors of HIVE.md.

The transport and the encoding are in D232. The DSN is still open.
