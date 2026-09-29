# D94. Every HTTP driver takes its secret as the password

Status: Amended by D110.

D110 says how this applies to each driver, and names SurrealDB as the one
exception.

Ken decided on 2026-09-29 one convention for the credentials of every
driver here that speaks HTTP. Gemini and DeepSeek recommended it on the same
day.

- The secret is the password of the URL: `<driver>://user:<secret>@host/...`.
  It is a password, or a token that the server takes in place of one. It is
  never a key of the query, because a query leaks into logs and proxies, and
  `url.URL.Redacted` hides only the password.
- The key `auth` says how the driver sends the secret:
  - `basic` sends the user and the secret with basic authentication.
  - `bearer` sends `Authorization: Bearer <secret>`, for a JWT or a token of
    single sign on, and sends no user.
- A driver has no mode that trades the user and the password for a token and
  renews it. A server that takes basic authentication needs none, and a
  caller who holds a token sends it with `bearer`.
- The default is `basic`, except for a server that takes only a Bearer token,
  such as libSQL and Turso, where it is `bearer`.

ArangoDB takes it from its first release (D93). The InfluxDB driver has no
key `auth` yet, and sends basic authentication only (D82). W14 in
[BACKLOG.md](../BACKLOG.md) adds the key to it.
