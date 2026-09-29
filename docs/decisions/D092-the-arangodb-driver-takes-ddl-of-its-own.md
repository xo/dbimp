# D92. The ArangoDB driver takes DDL of its own

Status: Decided.

Ken decided on 2026-09-29 that the ArangoDB driver takes statements that
make and drop collections and indexes, because AQL has no DDL, and the server
does all of it through HTTP endpoints (measured on 3.12.12). As the InfluxDB
driver takes INSERT of line protocol (D85), the driver reads the first words
of each statement, and turns each of these into one HTTP call:

| Statement | Call |
| --- | --- |
| `CREATE COLLECTION <name> [EDGE]` | `POST /_api/collection` |
| `DROP COLLECTION <name>` | `DELETE /_api/collection/<name>` |
| `CREATE [UNIQUE] [SPARSE] INDEX <name> ON <collection> (<field>, ...)` | `POST /_api/index`, a persistent index |
| `CREATE GEO INDEX <name> ON <collection> (<field>, ...)` | `POST /_api/index`, a geo index |
| `CREATE INVERTED INDEX <name> ON <collection> (<field>, ...)` | `POST /_api/index`, an inverted index |
| `CREATE TTL INDEX <name> ON <collection> (<field>) EXPIRE AFTER <seconds>` | `POST /_api/index`, a TTL index |
| `DROP INDEX <name> ON <collection>` | `DELETE /_api/index/<collection>/<id>` |

- Each statement takes `IF NOT EXISTS` after `CREATE ... COLLECTION` or
  `INDEX`, and `IF EXISTS` after `DROP COLLECTION` or `DROP INDEX`.
- A keyword can be in any case. A name can be in backticks, as in AQL.
- Every other statement is AQL, and goes to the cursor API.
- A statement of this form runs outside a transaction, because the server
  does not make a collection or an index inside one.
