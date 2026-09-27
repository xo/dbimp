# D2. The go directive is 1.27.1

Status: Decided.

`go.mod` says `go 1.27.1` and has no `toolchain` line. That is Ken's target,
and `dbmeta`, `cql` and `n1ql` use the same line.
