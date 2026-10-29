# D218. The MaxCompute driver is `maxcompute` and takes a URL

Status: Decided.

The package is `github.com/xo/dbimp/maxcompute`, and it registers the one name `maxcompute` with
`database/sql` (D26, D28 and D30). It registers no alias, and `dburl` keeps `mc` (D5). The DSN is
the form that `dburl` v0.54.0 builds (dburl D68), which Ken accepted on 2026-10-29:

```
maxcompute://<access key id>:<access key secret>@service.<region>.maxcompute.aliyun.com/<project>
```

1. The user is the access key id, and the password is the secret (D94). The driver reads no
   environment variable for either, and it reads no key from the query (D7).
2. The path is the project. A host of another form needs the key `region`. Without it, the
   driver reads the region from the host name, which is the second label of
   `service.<region>.maxcompute.aliyun.com`.
3. The driver adds `/api` to the path of each request, and `dburl` adds no host prefix or suffix.
4. `tls` is read as in D50. It is on by default, because the service is hosted on HTTPS. `tls=false`
   sends plain HTTP, for a proxy.
5. A key that starts with `odps.` is a setting of the server (D220). Any other key that the driver
   does not name is refused, and so is a repeated key.
6. The driver signs each request with signature version 4, never with version 2 (D221). The
   signature needs the region of item 2.

Ken decided items 1 to 3 and the form on 2026-10-29. See `docs/MAXCOMPUTE.md`, sections "The DSN"
and "Requests".
