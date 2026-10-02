# D169. The DynamoDB driver

Status: Decided.

These are the items of step 9 of [DRIVER.md](../DRIVER.md) for Amazon
DynamoDB (W30). Each fact is in [DYNAMODB.md](../DYNAMODB.md). D163 decides
that the driver serves no Alternator, and how it orders its columns.

1. The package and the name that it registers are `dynamodb` (D26 and D28).
   The scheme of `dburl` is `godynamo`, with `dynamodb` as an alias, so the
   scheme `dynamodb` moves to this driver, as D28 and D30 say.
2. The DSN is `dynamodb://key:secret@host:port?region=us-east-1`, with the
   endpoint as the host and no path. The keys are `tls` (true by default),
   and `region`, which has no default and must be set. Any other key is
   refused. The secret is the password of the URL (D94). Ken decided this form on
   2026-10-02, in place of the form of `dburl` today, whose host is the
   region and whose endpoint is a key.
3. The Go types are the type table of DYNAMODB.md. A number `N` is an
   `*apd.Decimal`, because DynamoDB names it an exact decimal of 38 digits.
   A column has no type, so each scan type is `any`, as for SurrealDB.
4. NULL and a missing attribute are one value, nil. The columns come from
   the statement (D163).
5. The server binds each `?` as a typed value. The driver refuses a count of
   arguments that is not the count of the placeholders, because the server
   takes too many with no error.
6. `BeginTx` returns `dbimp.ErrNotSupported`, as Ken decided on 2026-10-02.
   DynamoDB runs a transaction only as one request of reads only or of
   writes only, `ExecuteTransaction`, which `database/sql` cannot express.
7. The driver reads each page and follows `NextToken` to the end. A page can
   be empty and still carry a token.
8. Each request is one page and ends fast, so the context stops the request,
   and the server has nothing to cancel.
9. The driver follows no redirect, and sends the signature to the host of
   the DSN only. It signs each request with AWS Signature Version 4, written
   with the standard library.
10. The driver serves no flavor (D163).
11. The driver uses JSON only.
