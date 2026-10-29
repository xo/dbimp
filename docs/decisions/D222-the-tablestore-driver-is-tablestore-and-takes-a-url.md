# D222. The Tablestore driver is `tablestore` and takes a URL

Status: Decided.

The package is `github.com/xo/dbimp/tablestore`, and it registers the one name `tablestore` with
`database/sql` (D26, D28 and D30). `dburl` names the scheme `tablestore` and keeps `ots` as its
alias (D5). Ken decided the name on 2026-10-29. The DSN is the form that `dburl` v0.54.0 builds
(dburl D68):

```
tablestore://<access key id>:<access key secret>@<instance>.<region>.ots.aliyuncs.com/<instance>
```

1. The user is the access key id, and the password is the secret (D94). The driver reads no
   environment variable for either.
2. The path is the instance, which is also the first label of the host. `dburl` adds
   `.ots.aliyuncs.com` to a host of the form `<instance>.<region>`.
3. `tls` is read as in D50, on by default.
4. Any other key that the driver does not name is refused, and so is a repeated key.
5. The request is protobuf, and the rows of an answer are FlatBuffers. Ken approved on 2026-10-29
   that the driver writes the encoding and the decoding of both in this repository, with no
   package (D13). The code lives in the package `tablestore`.
6. The driver signs with the first version of the signature, the `x-ots-signature` header. The
   version 4 form is not measured.

See `docs/TABLESTORE.md`, sections "Requests" and "The DSN".
