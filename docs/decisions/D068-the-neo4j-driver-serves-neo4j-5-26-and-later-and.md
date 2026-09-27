# D68. The Neo4j driver serves Neo4j 5.26 and later, and has no flavors

Status: Decided.

Ken accepted this on 2026-09-27. It decides item 11 of step 9 for Neo4j. The
Query API is on by default from 5.25, and explicit transactions arrived in
5.26 (the manual). The floor of `dbrun` is 5.26.31. A release without the
Query API answers HTTP 404, and the driver returns it. Memgraph, FalkorDB and
Amazon Neptune do not speak the Query API (not measured), so the driver has no
flavors. `usql` reads the version with `CALL dbms.components()`, which works
for the ordinary user (measured), so the driver exports no `Version`.
