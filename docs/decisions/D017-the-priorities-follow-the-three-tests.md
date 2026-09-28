# D17. The priorities follow the three tests

Status: Amended by D84 and D87.

A target that meets R, H and S is P1. A target that fails one is P2.
A target that fails two or more, or that no longer exists, is P3. Inside
a priority, a target whose server sends column metadata, and a target that
`usql` has no driver for, come first.

Ken accepted this rule on 2026-09-27. The review in [TARGETS.md](../TARGETS.md)
applies it. It moves Pinot,
TDengine, Drill and Solr up to P1. It moves ArangoDB, Neo4j, Dgraph,
CouchDB, TerminusDB, Qdrant, Weaviate, ScyllaDB Alternator and Stargate down
to P2. It moves the MongoDB Atlas Data API, Fauna and PostgREST to P3.
The `dbmeta` session proposed the rule.
