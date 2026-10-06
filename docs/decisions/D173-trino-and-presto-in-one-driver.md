# D173. Trino and Presto in one driver

Status: Decided.

Ken named Trino and Presto as the next target on 2026-10-07, at step 1 of
[DRIVER.md](../DRIVER.md), and decided that one driver serves both.

- The package is `trino`, and it registers the name `trino` (D26 and D28).
  Presto is a flavor of it, as DRIVER.md names Trino and Presto an example of
  products that share one interface (Flavors). The driver tells the flavors
  apart from what the server answers, and never from the DSN alone.
- `dburl` has a scheme for each, `trino` and `presto`, with a `Dialect` for
  each. The scheme `presto` reaches this driver by its registered name, as
  DRIVER.md says (Flavors, and dburl D98).
- `dbrun` starts `trino-476`, `trino-483` and `presto-0.299`, and the tests
  run on every one (DRIVER.md, step 14).
- The two Go clients that `usql` uses now, `trinodb/trino-go-client` and
  `prestodb/presto-go-client`, are the drivers that this one can replace
  (D24).
- The driver goes through steps 2 to 9 of DRIVER.md as the other drivers did,
  and Ken decides its design in step 9.
