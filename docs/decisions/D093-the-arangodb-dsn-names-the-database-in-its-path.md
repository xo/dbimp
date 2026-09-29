# D93. The ArangoDB DSN names the database in its path

Status: Decided.

Ken decided on 2026-09-29 the name and the DSN of the ArangoDB driver. This
is items 1 to 3 of step 9 of [DRIVER.md](../DRIVER.md). The package is
`github.com/xo/dbimp/arangodb`, and it registers the one name `arangodb`
(D26, D28 and D30). The DSN is a standard URL (D27 and D35):

```text
arangodb://user:password@host:port/database?key=value
```

- The path is the database, and the driver sends each request to
  `/_db/<database>/`. With no path, the database is `_system`.
- Without a port, the driver uses 8529.
- The user and the password go as D94 says: basic authentication by
  default, or the password as a Bearer token with `auth=bearer`.

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true` to speak HTTPS | `false` |
| `cancel` | `tag`, `none` (D90) | `tag` |
| `batch` | the `batchSize` of each cursor, 1 or more | `1000` |
| `auth` | `basic`, `bearer` (D94) | `basic` |

Any other key is an error. Ken first chose a key `auth=jwt`, in which the
driver logs in at `/_open/auth` and renews the token. He replaced it on the
same day with the convention of D94, which has no login mode. A caller who
holds a JWT sends it with `auth=bearer`. The scheme and its aliases belong to
`dburl` (D5).

Note of 2026-09-29: a path of more than one segment is refused, as D61 does
for Neo4j (`dsn.go`).
