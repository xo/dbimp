# D148. The libSQL DSN names a server

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the libSQL driver. This is
items 1 to 3 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL. The package
is `github.com/xo/dbimp/libsql`, and it registers the one name `libsql`
(D26, D28, D30 and D76). `turso` is an alias in `dburl`. The DSN is a
standard URL (D27 and D35):

```text
libsql://user:token@host:port?key=value
```

- The host names a server of `sqld`, or a database of Turso Cloud, such as
  `mydb-myorg.turso.io`. The URL has no path, and a path is refused.
- TLS is on by default, as both libSQL clients read a `libsql:` URL (the Go
  client and the TypeScript client). With TLS, the default port is 443.
  With `tls=false`, the URL must name a port, such as 8080 for `sqld`.
- The password is the secret (D94). It goes as `Authorization: Bearer` by
  default, which a JWT of `sqld` and a token of Turso Cloud need.
  `auth=basic` sends the user and the password by basic authentication,
  for `sqld` with `SQLD_HTTP_AUTH`. The name of the user is a label with a
  token, and the server reads only the token (LIBSQL.md, The DSN).

| Key | Values | Default |
| --- | --- | --- |
| `tls` | `true`, `false` | `true` |
| `auth` | `bearer`, `basic` (D94) | `bearer` |
| `namespace` | the name of a namespace, sent as `x-namespace` | none |

Any other key is an error. The scheme and its aliases belong to `dburl`
(D5).
