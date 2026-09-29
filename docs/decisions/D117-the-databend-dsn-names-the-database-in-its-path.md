# D117. The Databend DSN names the database in its path

Status: Decided.

Ken decided this on 2026-09-29, at step 9 of the Databend driver.
This is items 1 to 3, 10 and 11 of step 9 of [DRIVER.md](../DRIVER.md) for
Databend. The package is `github.com/xo/dbimp/databend`, and it registers
the one name `databend` (D26, D28 and D30). D76 decides that it replaces the
Go driver in `usql` and `dburl`. The DSN is a standard URL (D27 and D35):

```text
databend://user:password@host:port/database?key=value
```

- The path is the database, which the driver sends as `session.database`.
  With no path, the database is `default`, as the server gives it
  ([DATABEND.md](../DATABEND.md)). A path of more than one segment is
  refused, as D61 does for Neo4j.
- Without a port, the driver uses 8000, the HTTP port of the image, with
  `tls=true` too, as D111 does for InfluxDB.
- The user and the password go as D94 says: basic authentication by
  default, or the password as a Bearer token with `auth=bearer`.

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `auth` | `basic`, `bearer` (D94) | `basic` |
| `cancel` | `kill`, `none` (D123) | `kill` |
| `timezone` | a timezone of the server, such as `UTC` | the setting of the server |

Any other key is an error, and so are the keys of databend-go, such as
`sslmode`, `tenant` and `warehouse`. The headers of Databend Cloud, which
those keys set, are a flavor that fails R, so the driver serves the one
product that `dbrun` starts, and no flavor (item 11). The driver follows no
redirect, and sends the credentials to the host of the DSN only (item 10),
through `dbimp.NewClient`. The scheme and its aliases belong to `dburl`
(D5), which stages them in dburl D39.

By D109, `WithDatabase` sets the database of one statement, `WithTimeout`
sets `max_execute_time_in_seconds`, and `WithCancel` and `WithTimezone` set
the keys of the table. The server has no read-only mode for one statement,
so `WithReadonly(true)` fails with `dbimp.ErrNotSupported`.
