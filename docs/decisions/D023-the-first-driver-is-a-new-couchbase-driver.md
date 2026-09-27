# D23. The first driver is a new Couchbase driver

Status: Decided.

Ken decided this on 2026-09-27. Couchbase, through its query service and
SQL++ (N1QL), is the first target in P1. This repository gets a clean
implementation, and it replaces `xo/n1ql`. It is not a port of `go_n1ql`,
and it keeps none of the three faults that D8 names.

dbmeta D94 ties the Couchbase model to a rewrite of `xo/n1ql`. When this
driver works, `dburl`, `usql` and `dbmeta` move to it, and `dbmeta` measures
its model again, as dbmeta D93 did when Cassandra moved to `xo/cql`.
