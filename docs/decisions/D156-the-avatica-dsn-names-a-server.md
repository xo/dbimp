# D156. The Avatica DSN names a server

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the Avatica driver. This is
items 1 to 3, 10 and 11 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.
The package is `github.com/xo/dbimp/avatica`, and it registers the one name
`avatica` (D26, D28 and D30). The DSN is a standard URL (D27 and D35):

```text
avatica://user:password@host:port?key=value
```

- The host and the port name a server of Avatica: the standalone server or
  the Phoenix Query Server. The default port is 8765. The URL has no path,
  and a path is refused.
- The user and the password go in the `info` of `openConnection`, which the
  server passes to the database. With `auth=basic`, they also go by HTTP
  basic authentication, for a server that checks them itself.

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `auth` | `none`, `basic` | `none` |

Any other key is an error. The driver follows no redirect. It serves the two
flavors with one code, and tells them apart from nothing, because nothing
that it does differs between them (D157 to D159). Druid is no flavor (D154).
