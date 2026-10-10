# D197. The error of a driver matches ErrAuthentication

Status: Decided.

The `usql` session asked on 2026-10-11 for one way to ask any dbimp error whether
the server refused the credential, so that it can drop seven checks of its own
and its rule for HTTP 401 (W42). Ken decided on 2026-10-11 to implement it for all
the drivers in a consistent way.

1. The root package has the sentinel `ErrAuthentication`, a constant of the type
   `Error` with the text `authentication refused`. A caller writes
   `errors.Is(err, dbimp.ErrAuthentication)`.
2. The error of each driver has an `Is(target error) bool` method that returns true
   for `ErrAuthentication` when the fields that the server sent say that it refused
   the credential: a wrong password, key or token. It returns false for a refusal
   for lack of a permission. The rule is in the code of the driver and in its
   document, and a table test holds each case that step 6 recorded.
3. A `StatusError` with the code 401 matches `ErrAuthentication`, because 401 is the
   status for a request with no valid credential. A `StatusError` with the code 403
   does not, because a server sends 403 for both causes.
4. The root package has no function `IsAuth`, because `errors.Is` is the way of
   Go to test a class of error, and a second way would only confuse a caller.
   Two of four models were asked and both chose this form, and the others did not
   answer (the question is W42).
5. The gate `TestEveryDriverClassifiesAuthentication` fails when a driver never
   names `dbimp.ErrAuthentication`, or when its tests never name it.
