# D158. Avatica binds typed parameters

Status: Decided.

Ken decided this on 2026-10-01, at step 9 of the Avatica driver. This is
item 6 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.

- A statement with arguments sends `prepare`, and then `execute` with the
  whole handle and a `TypedValue` for each argument (AVATICA.md,
  Parameters). The driver first checks the count of the arguments against
  the parameters of the signature, because the server binds a missing one as
  NULL with no error.
- An `*apd.Decimal` goes with the `rep` `STRING`, because the server reads
  the `rep` `NUMBER` as a double and loses digits. HSQLDB converts the text
  exactly. Phoenix refuses it with `Type mismatch`, and so loses no digit.
- The driver writes each character outside ASCII in a request as an escape
  of JSON, `\uXXXX`, because the Phoenix Query Server misreads the raw bytes
  of UTF-8 (measured).
- Avatica has no named parameters, so a named argument is an error.
