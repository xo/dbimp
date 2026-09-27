# D25. The code uses encoding/json/v2 and the conventions of Go 1.27

Status: Decided.

Ken decided this on 2026-09-27. The module targets Go 1.27.1 or newer (D2),
and it uses the newest conventions of Go where they fit:

- JSON is read and written with `encoding/json/v2` and
  `encoding/json/jsontext`, never with `encoding/json`.
- A nullable value is `sql.Null[T]`, never a type such as `sql.NullString`.
- A UUID is `uuid.UUID` from the standard library.

The reason for `json/v2` is speed. `jsontext.Decoder` reads one token at a
time from the body of the response. A driver can then decode a result as it
arrives from the server, and hand each row to `database/sql` before the rest
of the result has arrived. The whole result is never held in memory, and the
keys of an object keep their order (D18). Go 1.27.1 builds both packages with
no `GOEXPERIMENT` setting. A build on 2026-09-27 showed this.
