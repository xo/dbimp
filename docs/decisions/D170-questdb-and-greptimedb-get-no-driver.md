# D170. QuestDB and GreptimeDB get no driver

Status: Decided.

Ken decided on 2026-10-07 that dbimp writes no driver for QuestDB and no
driver for GreptimeDB. It amends D73, which named QuestDB among the first
targets. It follows D88, which gave CrateDB no driver for the same reason.

- QuestDB speaks the PostgreSQL wire protocol on port 8812, and GreptimeDB
  speaks the MySQL and the PostgreSQL wire protocols, by their documents.
  Neither was measured here, and `dbrun` starts QuestDB 9.4.3 and 10.0.1
  only, and no GreptimeDB.
- `usql` reaches a database on either protocol with a driver that it ships
  already, `pgx` or `go-sql-driver/mysql`. A driver here would add no
  feature that those drivers lack, and one more driver to keep (D88).
- The HTTP endpoint of QuestDB, `/exec`, binds no argument, so it is a worse
  fit than the wire protocol (TARGETS.md).
- If `dbmeta` needs a model of either one, it reads it on the wire protocol,
  as it reads CrateDB (D88).
