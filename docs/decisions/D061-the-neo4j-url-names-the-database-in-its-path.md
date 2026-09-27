# D61. The Neo4j URL names the database in its path

Status: Decided.

Ken chose the form and accepted this on 2026-09-27. It decides item 3 of step
9 for Neo4j. The URL is `neo4j://user:pass@host:port/<database>`, such as
`neo4j://neo4j:pw@127.0.0.1:7474/dbmeta`. Every request of the Query API names
a database in its path (measured). The path is one segment, decoded by the
rules of `net/url`. With no path, the database is `neo4j`, which a new server
has, and which is the only database of the Community Edition. A path of more
than one segment is refused.

The default port is 7474. The key `tls=true` makes the driver speak HTTPS,
and then the default port is 7473 (the manual). The key `cancel` is D67.
Every other key, and a key given twice, is refused. The user and the
password go in basic authentication. A URL with no user sends no
authentication, for a server that runs with authentication off.
