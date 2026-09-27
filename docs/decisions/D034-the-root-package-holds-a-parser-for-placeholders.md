# D34. The root package holds a parser for placeholders

Status: Decided.

Ken decided this on 2026-09-27, and it answers Q7. Some servers bind no
arguments. For them, the root package holds a small parser that finds each
`?` and each `@name` in a statement. It skips string literals, quoted
identifiers and comments, so that a `?` inside a literal stays as it is. That
fault is in `go_n1ql`. The driver then puts each argument into the statement
as a literal, written by an escaper that the driver supplies for its product.
The escaper is never shared, because each product quotes in its own way. Hive
reads a doubled quote as two literals joined (dbmeta D78).

A driver whose server binds arguments sends them to the server, and does not
use the parser for that.

An existing package can replace the parser if one fits D13. None was found on
2026-09-27. The sanitizer of `pgx` is `pgx/v5/internal/sanitize`, which
cannot be imported, and `interpolateParams` in `go-sql-driver/mysql` is not
exported.
