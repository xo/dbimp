# D38. The Couchbase DSN

Status: Amended by D43 and D46.

Ken accepted this on 2026-09-27. D43 adds the key `durability_level`, and
D46 adds the key `txtimeout`.

The DSN is `couchbase://user:pass@host:port/?key=value` (D27 and D35). The
user information holds the credentials, which the driver sends with basic
authentication, and never in the `creds` parameter, which puts the password
in the body. The host and the port are the query service, with the port
8093 by default, or 18093 when `tls` is true. The path is empty or `/`, and
any other path is an error. The keys of the query are these:

- `tls`: `true` to speak HTTPS, `false` by default.
- `query_context`: the bucket and the scope that a collection named alone
  belongs to, such as `default:dbmeta._default`.
- `scan_consistency`: `not_bounded` by default, or `request_plus`.
- `timeout`: a duration, such as `30s`, that the server enforces with code
  1080.

The driver talks to the host of the URL only. It does not find the other
nodes of a cluster, because `dbrun` cannot test that, and a cluster in a
container answers with addresses that a client outside it cannot reach
([COUCHBASE.md](../COUCHBASE.md)). It follows no redirect, and none was seen.
It asks Ken before it adds a key.
