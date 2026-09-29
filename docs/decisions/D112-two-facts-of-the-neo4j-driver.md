# D112. Two facts of the Neo4j driver

Status: Decided.

Ken decided on 2026-09-29 to record these, which the code held and no
decision named:

- `CheckNamedValue` calls `Value` of a `driver.Valuer` itself, and refuses a
  struct. Typed JSON must know the type of each value (D62 and D63), and
  Cypher has no type for a struct. The Couchbase driver leaves a
  `driver.Valuer` to `database/sql`, and sends a struct as JSON (D40),
  because its values are JSON.
- The driver has no `IsValid`, because a connection holds no state on the
  server outside a transaction, and `database/sql` ends every transaction
  before it reuses the connection (D102). The Couchbase driver implements
  `IsValid`, which always returns true, for the same reason.
