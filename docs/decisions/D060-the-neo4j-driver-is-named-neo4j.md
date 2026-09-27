# D60. The Neo4j driver is named neo4j

Status: Decided.

Ken accepted this on 2026-09-27. It follows D26 and D30, and decides items 1
and 2 of step 9 of [DRIVER.md](../DRIVER.md) for Neo4j. The package is
`github.com/xo/dbimp/neo4j`, and it registers the one name `neo4j` with
`database/sql`, which is the name of the database. The scheme of its URL is
`neo4j` (D35), and the `Driver` of the scheme in `dburl` and the dialect in
`dbmeta` take the same name. `dburl` keeps the aliases `nj`, `neo` and `n4j`,
which Ken chose on 2026-09-27.

The tools of Neo4j write `neo4j://host:7687` for Bolt with routing. This
driver speaks HTTP, so a URL copied from those tools names the port of Bolt,
and the request fails. [NEO4J.md](../NEO4J.md) and the documentation of the
package say so.
