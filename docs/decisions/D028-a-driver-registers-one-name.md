# D28. A driver registers one name

Status: Decided.

Ken decided this on 2026-09-27. Each driver calls `sql.Register` once, with
one name. It registers no alias. A package outside this repository, such as
`dburl`, keeps any alias that it chooses for a database, and turns the alias
into the one name. D30 says which name that is.
