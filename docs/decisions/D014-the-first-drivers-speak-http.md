# D14. The first drivers speak HTTP

Status: Decided.

Ken decided this on 2026-09-27. The first drivers here are adapters and
clients for databases that have an HTTP or HTTPS interface: SQL, NoSQL and
others. Each one takes a query that a person can type by hand, such as SQL, a
dialect like SQL, JSON, or another query language, because that is what
`usql` sends.

Each driver is pure Go and never needs cgo, because `net/http` is all that it
needs. A consumer builds it with `CGO_ENABLED=0`, and `RequiresCGO` in
`dburl` is false for every one of them.
