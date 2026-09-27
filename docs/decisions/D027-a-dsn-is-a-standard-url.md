# D27. A DSN is a standard URL

Status: Decided.

Ken decided this on 2026-09-27. A driver takes only a standard URL as its
DSN, and parses it with `net/url`, by the rules of the Go standard library.
`dburl` then writes that URL for each scheme. A driver keeps no form of DSN
from an earlier driver, such as the list of hosts that `xo/cql` accepts or
the address of a cluster manager that `go_n1ql` accepts. A DSN that
`net/url` refuses is an error.
