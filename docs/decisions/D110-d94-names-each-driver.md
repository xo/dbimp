# D110. D94 names each driver

Status: Amends D94.

Ken decided on 2026-09-29 how D94 applies to the drivers that it did not
name. D94 gave every HTTP driver the key `auth=basic|bearer`, and only the
ArangoDB driver had it.

- The ArangoDB driver has it (D93).
- The InfluxDB driver gets it in W14 of [BACKLOG.md](../BACKLOG.md).
- The Neo4j driver gets it in W14 too. Its server offers Bearer in
  `Www-Authenticate` (recorded).
- The Couchbase driver stays with basic authentication, and has no key
  `auth`, because its query service takes no Bearer token.
- The SurrealDB driver keeps its key `auth`, which names the level of the
  user, `root`, `namespace` or `database`, as D51 decided and `v0.2.0`
  released. It is the one exception to D94.

A later driver takes `auth=basic|bearer`, as D94 says.
