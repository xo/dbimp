# D35. A DSN is a URL whose scheme is the name of the driver

Status: Decided.

Ken decided this on 2026-09-27, and it answers Q11. `dburl` hands each driver
a complete URL, and the scheme of that URL is the one name that the driver
registers (D28), such as `couchbase://`. A driver knows no alias and accepts
no other scheme. `dburl` turns every alias into that URL.

The driver reads the rest of the URL by D27: the user information, the host,
the path and the query keys. Whether it speaks HTTPS or HTTP is a key of the
query, or another part of the URL that its decisions of step 9 of
[DRIVER.md](../DRIVER.md) name. It is never a second scheme, because the driver
has one name.
