# D208. The Exasol driver binds arguments on the server

Status: Decided.

Ken answered the step 9 questions of [DRIVER.md](../DRIVER.md) for Exasol (W44) on 2026-10-11. This is his answer to item 6.

The server binds the `?` placeholders. The driver sends `createPreparedStatement`, then `executePreparedStatement`, then `closePreparedStatement` for a statement that has arguments. A statement of `database/sql` that was prepared keeps its handle until it closes.

- The driver sends each argument in the JSON form that the server reads for the declared type of its parameter. Decimals go as strings. A double goes in the form `1.0`, because the server refuses an integer for a double.
- The server takes positional parameters only. A named argument is an error that wraps `dbimp.ErrNotSupported`, before the driver sends anything.
- A VARCHAR parameter takes the text of any value, because the server gives that type to a placeholder in a select list.
- The driver counts the arguments after it takes out the options (D109), and a wrong count is an error that wraps `dbimp.ErrArguments`.
