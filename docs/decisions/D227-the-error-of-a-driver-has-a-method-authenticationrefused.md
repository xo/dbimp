# D227. The error of a driver has a method AuthenticationRefused

Status: Decided on 2026-11-02.

The `usql` session set one contract for a refused credential (W66 of `usql`), and Ken told this
session on 2026-11-02 to implement it. The contract is a method on the error type and not a sentinel.
`usql` declares the interface on its own side, as the standard library does with the method `Timeout`
of `net.Error`, so a driver imports nothing from `usql`. D197 gave `ErrAuthentication` to a caller
that imports this module. This decision gives the same answer to a caller that does not.

1. Each error type of the module that has an `Is` method for `ErrAuthentication` also has the method
   `AuthenticationRefused() bool`. It returns what `Is(ErrAuthentication)` returns. It is true when a
   new password can succeed: a wrong password, key or token, or a credential that the server needs
   and the client did not send. It is false for a refusal for lack of a permission, a locked account,
   an expired password, a network, TLS or timeout failure, a database that does not exist, and a bad
   DSN. This is the line that D197 draws, and no case moves across it.
2. The root package has the method on `Error`, which is true only for `ErrAuthentication`, and on
   `*StatusError`, which is true only for HTTP 401. A driver that wraps the sentinel with
   `fmt.Errorf("%w: %w", dbimp.ErrAuthentication, err)`, as BigQuery and Spanner do for a refused
   token, needs no code, because the tree of the error holds an error that answers true.
3. The root package declares no interface and no function for the method. A caller reads it through
   an interface of its own, and walks the tree of the error through `Unwrap() error` and
   `Unwrap() []error`, because `errors.As` stops at the first error that has the method, even when
   that one returns false. A wrapper must use `%w`, because `%v` hides the method.
4. `ErrAuthentication` and each `Is` method stay, because a caller outside `usql` reads them.
5. The gate `TestEveryDriverClassifiesAuthentication` fails when a type has an `Is` method that names
   `dbimp.ErrAuthentication` and no method `AuthenticationRefused`. A test of the root package holds
   the two root types and a wrapped sentinel.
