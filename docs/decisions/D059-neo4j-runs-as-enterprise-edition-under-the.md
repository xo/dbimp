# D59. Neo4j runs as Enterprise Edition under the evaluation agreement

Status: Decided.

Ken decided this on 2026-09-27, in step 4 of [DRIVER.md](../DRIVER.md). The
Community Edition has no roles, and "all users have implied administrator
privileges" (the Operations Manual of Neo4j, read on 2026-09-27), so it has
no ordinary user (D31). The Enterprise Edition has roles. Its image starts
only with `NEO4J_ACCEPT_LICENSE_AGREEMENT`, which is `yes` for a commercial
licence, or `eval` for the Neo4j Software Evaluation Agreement (read in the
entrypoint of `neo4j/docker-neo4j` on 2026-09-27).

Ken accepted the evaluation agreement for the servers that `dbrun` starts,
including in CI. The agreement grants a licence for 30 days, for internal
development use only, and Neo4j collects data about the use of the software
unless a setting turns that off (read on 2026-09-27). The `dbmeta` session
records the acceptance in its own decision, as dbmeta D76 does for SAP HANA.

The image of 4.4 takes only `yes`, so 4.4 cannot run this way, and it is not
tested.
