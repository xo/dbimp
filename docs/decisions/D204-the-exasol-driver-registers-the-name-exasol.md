# D204. The Exasol driver registers the name exasol and no alias

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 2.

The driver registers the one name `exasol` with `database/sql`, from `init`, and no alias (D28 and D30). `dburl` has the aliases `ex` and `exa`, and it keeps them (D5).
