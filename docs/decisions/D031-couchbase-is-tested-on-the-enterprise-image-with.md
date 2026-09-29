# D31. Couchbase is tested on the Enterprise image, with an ordinary user

Status: Decided.

Ken accepted this on 2026-09-27. `dbmeta` starts Couchbase from the image
`docker.io/library/couchbase`, whose bare tag is the Enterprise edition,
which is free for development. The `Init` step of that entry creates only
`Administrator` today. Step 4 of [DRIVER.md](../DRIVER.md) needs an ordinary user
as well, so the `dbmeta` session adds one, with a password that `dbrun`
knows.

Note of 2026-09-29: the `dbmeta` session added the ordinary user
`dbmeta_user` (dbmeta D96), and the tests read it from
`COUCHBASE_ORDINARY_DSN`.
