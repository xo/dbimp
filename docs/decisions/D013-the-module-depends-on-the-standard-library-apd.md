# D13. The module depends on the standard library, apd, and approved binary encodings

Status: Decided.

Ken decided this on 2026-09-27. The module is close to free of dependencies.
It has three kinds of dependency, and no other:

1. The Go standard library. HTTP, JSON, TLS, authentication and request
   signing are written with it. That includes the AWS Signature Version 4
   that DynamoDB needs, and the parser for placeholders of D34.
2. `github.com/cockroachdb/apd/v3`, which holds a decimal (D33).
3. A package for a binary encoding that the standard library does not have,
   such as CBOR, when Ken approves it for one database.

A binary encoding is considered for each database on its own, and Ken
decides each case. Two questions decide it: whether the package for the
encoding can come into this module with few dependencies, and whether the
gain is worth the cost of keeping it. Examples are CBOR for SurrealDB,
VelocyPack for ArangoDB, Smile for Elasticsearch, and the Arrow stream for
InfluxDB 3 and Databricks. Most targets in [TARGETS.md](../TARGETS.md) also
answer in JSON, so a binary encoding needs a reason that JSON cannot meet.

Ask Ken before you add any package. When he approves one, add it to the
`depguard` list in `.golangci.yml`, so that the list is the record of every
package that the module can import.

Ken approved CBOR for SurrealDB on 2026-09-27, written in the root package
with the standard library, so it adds no package (D49).
