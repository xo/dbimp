# D64. The Neo4j driver binds ordinal n as the parameter $n

Status: Decided.

Ken chose this and accepted it on 2026-09-27. It decides item 6 of step 9 for
Neo4j. The server binds named parameters, and takes a name made of digits,
such as `$0` (measured). So a positional argument with the ordinal n is the
parameter `"n"`, and a caller writes `$1`, `$2` and so on in the statement.
`sql.Named("k", v)` is the parameter `"k"`. The driver never parses or
rewrites the text of a statement to bind an argument. A name that the
statement does not use is ignored by the server, and a name that the statement
uses without a value is HTTP 400 with
`Neo.ClientError.Statement.ParameterMissing` (measured).
