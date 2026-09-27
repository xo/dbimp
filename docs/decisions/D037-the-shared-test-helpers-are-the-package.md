# D37. The shared test helpers are the package dbimptest

Status: Decided.

Ken accepted this on 2026-09-27. D4 puts the code that the drivers share in
the root package. The helpers for the tests of a driver are the exception.
They are the package `github.com/xo/dbimp/dbimptest`, which only a test
imports.

The helpers import `testing` and `net/http/httptest`. Every driver imports
the root package. If the root package held the helpers, every consumer of a
driver links both packages and runs their `init`, for code that only a test
uses. The root package keeps what a driver needs at run time,
and `dbimptest` keeps what it needs in a test.

The first reason written here was that `httptest` adds a command line flag
to every program. The source of Go 1.27.1 showed that it adds the flag only
when the command line already names it, so that reason was wrong.

`dbimptest` is not a driver, so the gates of W6 skip its folder.
[DESIGN.md](../DESIGN.md) holds what it contains.
