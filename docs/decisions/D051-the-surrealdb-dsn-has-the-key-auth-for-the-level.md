# D51. The SurrealDB DSN has the key auth for the level of the user

Status: Decided.

Ken decided this on 2026-09-27, and it adds a key to D48. The key `auth` is
`root`, `namespace` or `database`, and the default is `root`. It says where
the user of the URL is defined. For `database`, the driver sends the headers
`Surreal-Auth-NS` and `Surreal-Auth-DB` with the names of the path, and for
`namespace` it sends `Surreal-Auth-NS`.

A database user is refused with HTTP 401 without those headers, and root is
refused with HTTP 401 with them (measured on 2.7.0 and 3.3.0). So the driver
cannot use one form for both, and it never tries a second form after a
failure. `dbrun` writes `?auth=database` into the URL of its ordinary user.
