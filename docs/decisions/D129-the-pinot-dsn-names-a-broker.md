# D129. The Pinot DSN names a Broker

Status: Decided.

Ken decided this on 2026-09-30, at step 9 of the Apache Pinot driver. This
is items 1 to 3, 10 and 11 of step 9 of [DRIVER.md](../DRIVER.md). The
package is `github.com/xo/dbimp/pinot`, and it registers the one name
`pinot` (D26, D28 and D30). The DSN is a standard URL (D27 and D35):

```text
pinot://user:password@host:port?key=value
```

- The host and the port name a Broker, which runs each query. Pinot has
  tables and no databases, so the URL has no path, and a path is refused.
- Without a port, the driver uses 8099, the port of a Broker, with
  `tls=true` too. The QuickStart image that `dbrun` runs puts its Broker on
  8000.
- The user and the password go as D94 says: basic authentication by
  default, or the password as a Bearer token with `auth=bearer`.

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `auth` | `basic`, `bearer` (D94) | `basic` |
| `cancel` | `kill`, `none` (D133) | `kill` |
| `engine` | `multi`, `single` (D131) | `multi` |

Any other key is an error. The driver follows no redirect, and sends the
credentials to the host of the DSN only (item 10). It serves Apache Pinot,
and no flavor (item 11). The scheme and its aliases belong to `dburl` (D5),
which holds the provisional scheme `pinot` (dburl D36).
