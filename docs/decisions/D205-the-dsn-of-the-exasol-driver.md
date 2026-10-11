# D205. The DSN of the Exasol driver

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 3.

The DSN is a standard URL that `net/url` parses (D27). Its form is `exasol://user:password@host:port/schema?key=value`.

- The scheme is `exasol`. The host is required, and the port is 8563 by default.
- The user is required. The password is the secret of the URL, and no key of the query holds a secret (D94).
- The path is the schema that the session opens at the login. It can hold one name.
- A key that is unknown or repeated is an error (D27). The keys of `exasol-driver-go` that this driver does not take, such as `encryption`, `user` and `password`, are errors.

The keys, each with its default:

| Key | Default | Meaning |
| --- | --- | --- |
| `tls` | true | `false` makes the driver speak `ws` and not `wss`. |
| `validateservercertificate` | true | `false` accepts any certificate. |
| `certificatefingerprint` | none | The SHA-256 of the certificate, in hex. It takes the place of the check of the chain. |
| `compression` | true | See D214. |
| `autocommit` | true | The login sets the attribute `autocommit`. |
| `fetchsize` | 1024 | The size of a page of a result in KiB, from 1 to 65536 (D210). |
| `querytimeout` | 0 | Seconds that the server lets a statement run, from 0 to 999999. 0 means no limit. |
| `clientname` | `dbimp` | The name that the login sends. |
| `protocol` | 3 | The version of the WebSocket API that the login asks for: 3, 4 or 5. |

Ken named the key `protocol` and gave no default. The default 3 is the proposal of the driver. It is the version of every recording in `testdata/exasol/`, and the server answers the version that it was asked for when it is 3, 4 or 5. The question stays open in [EXASOL.md](../EXASOL.md).
