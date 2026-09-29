# D97. Every driver is compared with the first before its commit

Status: Decided.

Ken decided on 2026-09-29 that each driver is compared with `couchbase`,
the first driver, and that he reviews the comparison before the commit of
step 18. He asked for it after a comparison of the ArangoDB driver with the
Couchbase driver. That comparison showed a difference that a caller sees
and that no decision had named: Couchbase takes options for one statement,
and ArangoDB takes none.

Step 17a of [DRIVER.md](../DRIVER.md) holds the rule:

- The comparison is the section `## Compared with Couchbase` at the end of
  `docs/<PRODUCT>.md`. `### The server` compares the two interfaces, and
  `### The driver` compares the two Go packages.
- Under each table is the list of the differences that a caller of
  `database/sql` sees, each with the decision that gives its reason. A
  difference with no reason is a question for Ken.
- It is written after step 17, and before step 18, so that it compares the
  code as it is committed.
- `TestEveryDriverIsComparedWithTheFirst` checks that the section and its two
  parts exist. It cannot check that Ken read it, so the review is a stop of
  "Where you stop and ask Ken".

Ken also decided that day that SurrealDB, Neo4j and InfluxDB, which were
released before this rule, get their comparison too, and that the gate
names no driver as an exception other than `couchbase`.

Couchbase is the reference because it was the first driver, and the one that
the shared code grew from (D4 and D23). The rule does not make it the best
design. A comparison can show that Couchbase is the one that needs to
change.
