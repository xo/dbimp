# D230. The error of a driver has a method ErrVerbose

Status: Decided on 2026-11-08.

The `usql` session added the command `\errverbose`, as `psql` has it, and asked each driver of this module for
a method on its error type. `usql` cannot name a type of this module, so it reads the method through an
interface of its own, as it does for `AuthenticationRefused` (D227). It walks the chain of the error with
`errors.As`, and it takes the first error that has the method `ErrVerbose(query string) string`. Ken told
this session on 2026-11-08 to implement it.

1. The exported error type of each driver has the method `ErrVerbose(query string) string`. It returns the
   message on the first line, and then each field that the server sent, one on each line, as `Name: value`.
   It leaves out a field that has no value. It returns "" when no field has a value but the message, and
   `usql` then prints the error as it does today. The method takes `query` as the interface needs it, and no
   driver uses it, because no server of this module names a position in a way that a person can read
   (Trino gives a line and a column, which the method writes as two fields).
2. The root package has `Verbose(message string, pairs ...any) string`, which writes that form, so that
   each driver writes the same lines. The pairs are a name and a value. A value is left out when it is
   the zero value of its type.
3. A response that holds several errors has one type for the response, and the type has the method too. It
   writes the status of the response, and then the text of each error. This covers `ResponseError` of
   Couchbase, Neo4j and SurrealDB.
4. The method does not write the body of the response, because the fields hold what the driver read from it.
5. The gate of step 12 fails for an exported type that has a method `Error` and no method `ErrVerbose`.
