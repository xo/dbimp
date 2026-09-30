# D141. The rqlite DSN names a node

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the rqlite driver. This is
items 1 to 3, 10 and 11 of step 9 of [DRIVER.md](../DRIVER.md) for rqlite.
The package is `github.com/xo/dbimp/rqlite`, and it registers the one name
`rqlite` (D26, D28 and D30). The DSN is a standard URL (D27 and D35):

```text
rqlite://user:password@host:port?key=value
```

- The host and the port name one node of a cluster. rqlite has one
  database and no namespace, so the URL has no path, and a path is refused.
- Without a port, the driver uses 4001, the port of the HTTP API.
- The user and the password go by HTTP Basic authentication, the only form
  that rqlite has (RQLITE.md, Requests). A URL with no user sends no
  credentials, for a node without `-auth` or with the user `*`.

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `level` | `none`, `weak`, `linearizable`, `strong`, `auto` | the default of the server, which is `weak` |
| `freshness` | a Go duration, for `level=none` | none |

Any other key is an error. The server takes an unknown `level` as `weak`
with no error (recorded), so the driver refuses a value that the table does
not name. `level` and `freshness` are options for one statement too (D109).

The driver sends no `redirect`, so a follower forwards a write to the
leader itself. It follows no redirect, and sends the credentials to the
host of the DSN only (item 10). It serves rqlite, and no flavor (item 11).
The scheme and its aliases belong to `dburl` (D5), which holds the
provisional scheme `rqlite` (dburl D36).
