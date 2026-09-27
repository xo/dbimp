# D62. The Neo4j driver speaks typed JSON, and asks for the best version

Status: Decided.

Ken accepted this on 2026-09-27. It decides item 12 of step 9 for Neo4j, and
how a statement is sent. The driver needs no binary encoding.

Each statement is `POST /db/<database>/query/v2`, with
`{"statement": ..., "parameters": {...}}`. The driver sends
`Accept: application/vnd.neo4j.query.v1.2, application/vnd.neo4j.query.v1.1;q=0.9, application/vnd.neo4j.query;q=0.8`,
and the server answers the best version that it has: v1.0 on 5.26.31 and
v1.2 on 2026.09.0 (measured). The driver reads the version from the
`Content-Type` of the response.

Plain JSON cannot tell a date, a point, a duration, NaN or a byte array from
a string, and drops the name of a zone (measured). Typed JSON keeps each
type, and it is text that a person can read while they debug. So the driver
has no key for plain JSON, and it does not use JSON Lines, which 5.26.31
refuses (measured) and which gives nothing that reading one token at a time
does not.

The parameters are typed JSON too. The `Content-Type` is the lowest version
that carries every parameter: `application/vnd.neo4j.query` for most,
`.v1.1` with a vector, and `.v1.2` with a UUID. A server that does not have
that version answers HTTP 415, and the driver returns that error (measured
on 5.26.31).
