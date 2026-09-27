# D47. The SurrealDB driver is named surrealdb

Status: Decided.

Ken accepted it on 2026-09-27, with D48. It follows D26 and D30, and decides
items 1 and 2 of step 9 of [DRIVER.md](../DRIVER.md) for SurrealDB. The package
is `github.com/xo/dbimp/surrealdb`, and it registers the one name
`surrealdb` with `database/sql`, which is the name of the database. The
scheme of its URL is `surrealdb` (D35), and the `Driver` of the scheme in
`dburl` and the dialect in `dbmeta` take the same name. `dburl` keeps any
alias, such as `surreal`.
