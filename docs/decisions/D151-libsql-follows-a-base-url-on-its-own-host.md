# D151. libSQL follows a base_url on its own host

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the libSQL driver. This is items
10 and 11 of step 9 of [DRIVER.md](../DRIVER.md) for libSQL.

- An answer can name a `base_url`, to which the next requests of its stream
  go (the Hrana spec). The driver follows it only when it has the scheme,
  the host and the port of the DSN, and refuses any other with an error. So
  the token never leaves the host of the DSN. It follows no HTTP redirect.
- The driver serves `sqld` and Turso Cloud, which speak the same Hrana (the
  Turso documents). Turso Cloud fails R, so the tests run on `sqld` only.
  Turso Database is in beta and has no entry in `dbrun`, so the driver does
  not claim it.
