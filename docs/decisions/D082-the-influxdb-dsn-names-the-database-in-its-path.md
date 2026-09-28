# D82. The InfluxDB DSN names the database in its path

Status: Decided.

Ken decided on 2026-09-28 the DSN of the `influxdb` driver, which is item 3
of step 9 of [DRIVER.md](../DRIVER.md). The DSN is a standard URL of the
form that `dburl` gives (D27 and D35):

```text
influxdb://user:password@host:port/database?key=value
```

The path is the database. The driver sends it as `db` to `/query` and to
`/api/v3/query_sql`. On InfluxDB 2, `/query` finds the bucket through the
mapping that the `Init` of the `dbrun` entry makes (dbmeta D114).

The driver reads these keys, and sends none of them to the server:

| Key | Values | Default |
| --- | --- | --- |
| `sqlmode` | `disable`, `allow`, `prefer`, `require` (D78) | `prefer` |
| `version` | `1`, `2`, `3` (D78) | `3` |
| `describe` | `always`, `disable` (D80) | `always` |
| `chunked` | `prefer`, `disable` (D83) | `prefer` |
| `rp` | the retention policy that InfluxQL reads, sent as `rp` | none |
| `tls` | `true` to speak HTTPS, as the `neo4j` driver does (D61) | `false` |

Any other key is an error. A key that the server reads, such as `epoch`,
is set by the driver and not by the caller.

## Authentication

The driver sends the user and the password with basic authentication on
every request, and on `GET /ping` too. Step 6 measured that each release
takes it:

- InfluxDB 1 takes a user and its password.
- InfluxDB 2 takes a v1 user and its password, or any user with a token as
  the password.
- InfluxDB 3 takes any user with a token as the password. `GET /ping`
  answers HTTP 401 without it, on 3.9.13 and 3.11.5.

So a token is the password of the URL, and the driver has no other form of
authentication. With no user and no password, the driver sends no
credentials, for a server that runs with authentication off.

## The port

Without a port, the driver uses 8086 when `version` is `1` or `2`, and 8181
otherwise. These are the ports that `dburl` names (dburl D29). With
`sqlmode` set to `prefer` or `require`, the release is not known before the
ping, so the default of `version`, `3`, chooses the port.
