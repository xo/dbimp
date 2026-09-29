# D109. Every driver takes the same options

Status: Amends D40.

Ken decided on 2026-09-29 that every driver takes options for one
statement, and that the drivers look alike, as one implementation. Only the
Couchbase driver had them (D40). The rule:

- The machinery lives once in the root package, with generics:
  `dbimp.Option[T]`, `dbimp.WithOptions` and `dbimp.Resolve`. Each driver
  names its own options type, and aliases the machinery for it, as
  `type Option = dbimp.Option[options]`, so that the options of two drivers
  cannot be mixed.
- An option comes from the DSN, then from the context through `WithOptions`,
  then from an argument of the type `Option`, and a later one wins, as D40
  says. `CheckNamedValue` takes an `Option` out of the arguments.
- Four options have the same name and meaning in every driver:
  - `WithTimeout`, the time that the server gives one statement;
  - `WithReadonly`, which makes the server refuse a write;
  - `WithParameter`, which sets any key of the body of the request by its
    name, for a setting that the driver has no option for;
  - `WithDatabase`, the database of one statement, where the request names
    it.
- A driver also has an option for each key of its DSN that can change for
  one statement, with the same meaning, such as `batch` and `cancel` of
  ArangoDB, or `rp`, `chunked` and `describe` of InfluxDB. A key of the
  connection, such as `tls` or `auth`, is no option.
- A common option that the server of a driver cannot honor fails the
  statement with an error that wraps `dbimp.ErrNotSupported` and names the
  option, so that a caller never believes that a limit holds when it does
  not. `WithTimeout` on Neo4j 5.26, which ignores `maxExecutionTime`
  (measured), is such a case.

The Couchbase driver moves to the machinery of the root package, and keeps
its options. W15 in [BACKLOG.md](../BACKLOG.md) is the work, with the option
of each server that each driver maps to.

Note of 2026-09-29: W15 settled these points of the rule, so that the
drivers stay alike:

- `WithParameter` replaces a key that the driver sets itself, in every
  driver, as it does in Couchbase (D40). `dbimp.MarshalParams` writes the
  keys for a body of JSON.
- A statement of a transaction runs in the database of the transaction.
  `WithDatabase` with another database fails with `dbimp.ErrNotSupported`,
  on Neo4j and on ArangoDB.
- Couchbase has no database, so its `WithDatabase` sets `query_context`, as
  `WithQueryContext` does.
- An option whose value the DSN would refuse, such as a negative timeout,
  fails the statement with `dbimp.ErrInvalidValue`, and sends nothing.
- `WithReadonly(false)` and a timeout of zero ask for nothing, so they never
  fail with `dbimp.ErrNotSupported`.
