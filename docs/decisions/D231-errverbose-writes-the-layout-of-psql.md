# D231. ErrVerbose writes the layout of psql

Status: Decided on 2026-11-08.

D230 gave each driver a method `ErrVerbose`, and wrote the message and then `Name: value` lines. The
`usql` session reported that Ken wants the output of `\errverbose` to look as close as possible to the
output of `psql` for every database, so that it is the same across them. This decision replaces the
format of items 1, 2 and 4 of D230. The method, its name and its signature stay, and so do the gate and
the rule that a response with several errors has a method of its own.

1. The text has the layout that libpq writes in its verbose mode (the reference is `drivers/pgwire/pgshared/errverbose.go`
   of `usql`). The first line is `ERROR:` and two spaces, then the code and a colon and a space if the
   server gave a code, then the message. With no code, the line is `ERROR:  ` and the message.
2. When the server gives a position, the next two lines are `LINE n: ` with that line of the query, and a
   caret under the character, which is indented by the length of `LINE n: ` and the column. Only Trino gives
   a line and a column today. A server that gives only an offset can use the same helper later. The method
   takes `query` for this, and the other drivers still do not use it.
3. Each other field follows on its own line, as its name in upper case, a colon, two spaces and the value.
   A field with no value is left out. The text has no newline at its end.
4. The mapping for a driver, for each field of its error type: the code goes on the first line, and the
   SQLSTATE goes there when the server gives one and the native code becomes the field `ERROR CODE` or
   `DETAIL`. The name of a code, such as `UNKNOWN_TABLE`, goes in `DETAIL`. A stack trace goes in
   `CONTEXT`. Every other field keeps its name in upper case, such as `HTTP STATUS` and `QUERY ID`.
5. The root package has `Verbose(code any, message, position string, pairs ...any) string`, which writes
   this layout, and `Position(query string, line, column int) string`, which writes the two lines of
   item 2. A response with several errors writes each error, and then the fields of the response.
6. The method returns "" only when the server sent no code, no message and no field with a value. `usql`
   then prints the error as it does today.
7. A server can repeat the position in its message. Trino writes `line 2:3: ` before the text. When the
   method writes the `LINE` line, it leaves that prefix out of the message on the first line, because the
   `LINE` line and the caret show the place. It leaves the prefix in when the query is empty, and when
   the prefix names another place (the `usql` session checked this on Trino 476, on 2026-11-08).
