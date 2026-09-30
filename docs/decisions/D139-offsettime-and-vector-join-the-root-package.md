# D139. OffsetTime and Vector join the root package

Status: Amends D63 and D138.

Ken decided this on 2026-09-30, after W21 left `neo4j.Time` and
`neo4j.Vector` as the last types of their kinds in a driver.

- A time of day with an offset, such as the `TIME` of Neo4j and the
  `TIME WITH TIME ZONE` of SQL, is a
  `dbimp.OffsetTime{Time LocalTime; Offset int}`, with `Offset` in seconds
  east of UTC. It replaces `neo4j.Time`. A plain `time.Time` on 0000-01-01,
  as lib/pq gives a `TIMETZ`, would look like a timestamp in `*any`, and a
  `time.Time` argument goes to Neo4j as an `OffsetDateTime`, so a caller
  could not send a time of day with an offset.
- A vector is a `dbimp.Vector[T]`, a slice of `int8`, `int16`, `int32`,
  `int64`, `float32` or `float64`, in both directions. It replaces
  `neo4j.Vector`, and the plain slice that D138 named for a vector that the
  server returns. A caller sends the type that it read, so the round trip is
  the same both ways. A plain slice stays a list as an argument (D63),
  because a list of numbers is common in Cypher, and 5.26.31 of Neo4j has no
  vector.
- `dbimp.Vector[T]` is a slice, so a value scans into a `*[]float32` too.
  Databend gives its `Vector` as a `dbimp.Vector[float32]`.

The custom value types that stay in a driver are `surrealdb.RecordID`, and
`neo4j.Point`, `neo4j.Node`, `neo4j.Relationship` and `neo4j.Path`, because
only one driver has each one, or the databases differ in what it holds.
