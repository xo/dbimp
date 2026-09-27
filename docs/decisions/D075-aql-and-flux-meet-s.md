# D75. AQL and Flux meet S

Status: Amends D16.

Ken decided on 2026-09-28 that AQL, the query language of ArangoDB, and
Flux, the query language of InfluxDB 2, meet S. It amends S of D16, as D58
did for a graph query language.

AQL has statements such as `FOR`, `FILTER` and `RETURN` (not measured, from
[ARANGODB.md](../ARANGODB.md)). Flux is a chain of functions, such as
`from(bucket: ...) |> range(start: -5m)` (not measured, from
[INFLUXDB.md](../INFLUXDB.md)). A person types each one by hand, which is
what S asks for.

So ArangoDB leaves the list of targets that fail S in
[TARGETS.md](../TARGETS.md). Whether the InfluxDB driver serves InfluxDB 2,
and so Flux, is still a decision of step 9 of that driver. This decision
says only that Flux does not rule InfluxDB 2 out.
