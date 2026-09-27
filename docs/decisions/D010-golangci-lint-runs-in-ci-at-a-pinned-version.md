# D10. golangci-lint runs in CI at a pinned version

Status: Decided.

`.golangci.yml` sets `version: "2"` and `default: all`, with a disable list.
Each disabled linter has its reason beside it. The version is pinned in the
workflow, so that a new release cannot turn on a linter that nobody chose.
This is the posture of `dburl`, `dbmeta` and `cql`.

A linter that makes idiomatic Go worse is disabled. Only a real defect gets a
code change. A change made only to quiet a linter is itself a defect.
