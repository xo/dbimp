# D84. Gel, Blazegraph and Stargate leave the targets

Status: Amends D17 and D73.

Ken decided on 2026-09-28, in dbmeta D118, that a product that is dead,
retired or no longer supported is removed from every `xo` project when that
is convenient. He named Gel, Blazegraph, Stargate and Apache Impala, and
none of them gets a `dbrun` entry. The `dbmeta` session relayed the rule to
this repository on the same day.

[TARGETS.md](../TARGETS.md) named three of them:

- Gel, which was EdgeDB, item 20 of the first list and a row of
  `targets.csv`.
- Blazegraph, one of the SPARQL servers of item 19 of the first list.
- Cassandra through the Stargate API, item 10 of the first list and a row
  of `targets.csv`. D17 moved it to P2, and D73 said that it keeps its row.

Each one leaves the lists, the review and the tables of TARGETS.md. The
other items of the first list keep the numbers that Ken wrote, so the
numbers 10 and 20 are gone. Apache Impala was never a target here.

Ken also chose in dbmeta D118 that SingleStore gets no `dbrun` entry. That
choice does not name it as dead, so SingleStore keeps its row, and it fails
R because `dbrun` does not start it.
