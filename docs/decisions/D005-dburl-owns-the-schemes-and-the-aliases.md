# D5. dburl owns the schemes and the aliases

Status: Decided.

`dburl` owns the taxonomy of schemes, aliases and flavors. This repository
never writes a list of schemes or of aliases. A consumer that holds a URL in
any form reads `dburl` for it, and `dburl` hands the driver one complete URL
(D35). This follows `dbmeta` hard rule 1.

Each driver calls `sql.Register` from `init` with one name, which is the name
of its package and of its database (D28 and D30). The scheme of the URL that
it reads is that name. The driver parses that URL with `net/url` (D27), and
nothing else.

When a driver lands here, the `dburl` session changes its scheme: the
`GoPackage` becomes `github.com/xo/dbimp/<driver>`, `RequiresCGO` says
whether it needs a C compiler, and the `Driver` name becomes the name of the
driver if it differs (D30). Step 16 of [DRIVER.md](../DRIVER.md) is that move.
