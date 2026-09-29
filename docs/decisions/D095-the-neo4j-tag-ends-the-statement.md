# D95. The Neo4j tag ends the statement

Status: Amends D67.

Ken accepted this on 2026-09-29, after the tests of `usql` against `v0.4.0`.
With `cancel=tag`, D67 put the comment `/* dbimp:<id> */ ` at the start of
each statement. That moved each position of an error by the length of the
comment, and the message of the error showed the comment. A caller could
not find the place of the fault in the text that the caller wrote.

The driver now ends each statement with the comment on a line of its own:

```text
<the statement of the caller>
// dbimp:<id>
```

The id is still random and fixed for the connection, so the plan cache of
the server stays warm, as D67 measured. The driver finds the statement with
`SHOW TRANSACTIONS` by `currentQuery ENDS WITH $tag`. These facts were
measured on 5.26.31 and 2026.09.0:

- `currentQuery` keeps the comment at the end, and `ENDS WITH` finds the
  statement. `TERMINATE TRANSACTION` then stops it, as the administrator and
  as the ordinary user.
- A syntax error in the text of the caller keeps its position. In
  `RETURN 1 AS a,, 2`, the error is at line 1, column 15, and the message
  holds no comment.
- An error at the end of the input, such as `RETURN 1 +`, points at the line
  of the comment, and shows it. This is the one case that still shows it.
- A statement that ends with a `//` comment of the caller still works,
  because the tag starts a new line.

The ArangoDB driver puts its tag at the end for the same reason (D90), and
at the start of a long query, which its list of running queries cuts
(D99). Its
tag differs for each query, because a connection can hold two cursors at
once. The Neo4j tag stays fixed for the connection, for the plan cache. So
when one `sql.Conn` holds two open results outside a transaction, and the
context of one ends, the driver can stop both. D67 had the same limit. Inside a
transaction, both statements are in one transaction of the server, and
`TERMINATE TRANSACTION` ends it all in any case.

`cancel=metadata` does not change. This changes a released driver, so a
caller of `v0.4.0` sees the new text in `SHOW TRANSACTIONS` from the next
release.
