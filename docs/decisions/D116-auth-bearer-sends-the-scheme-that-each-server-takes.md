# D116. auth=bearer sends the scheme that each server takes

Status: Amends D110.

Ken decided this on 2026-09-29, when W14 of [BACKLOG.md](../BACKLOG.md) gave
the InfluxDB and Neo4j drivers the key `auth` of D94. `auth=bearer` means
that the secret of the URL is a token, and the driver sends it in the
header `Authorization`, with no user, in the scheme that the server takes.
Each server answered with the secret of the administrator of dbrun on
2026-09-29:

- InfluxDB 1.13.1 answered "bearer auth disabled". It takes `Bearer` only
  for a JWT, when its server holds a shared secret, which dbrun sets up for
  no server. The driver sends `Bearer`.
- InfluxDB 2.9.1 refused `Bearer`, and took `Token <token>`. The driver
  sends `Token`.
- InfluxDB 3.11.5 took both, for InfluxQL, SQL and the ping. The driver
  sends `Bearer`.
- Neo4j 5.26.31 and 2026.09.0 answered "Unsupported authentication token:
  scheme='bearer'". Neo4j takes a Bearer token only from an identity
  provider of single sign on, which dbrun runs for no server. The driver
  sends `Bearer`, and only the refusal is tested.

The InfluxDB driver learns the release before it sends a token. The ping,
which it sends before it knows the release, carries `Bearer`, and InfluxDB
1 and 2 answer it with any credentials or none. `dbimp.SetAuth` sends the
header for each driver, with the scheme that the driver names.
