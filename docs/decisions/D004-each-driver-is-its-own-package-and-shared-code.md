# D4. Each driver is its own package, and shared code is in the root package

Status: Decided.

Ken decided this on 2026-09-27. A driver is the package
`github.com/xo/dbimp/<driver>`, in a folder at the root with the name of the
driver. That import path is the `GoPackage` of its scheme in `dburl`. A
consumer imports each driver it wants directly, so a driver that nobody
imports costs a consumer nothing.

The root package `dbimp` holds the code that the drivers share: the HTTP
client, the adapters that encode and decode values, and the other utilities
that two or more drivers use. Every driver imports it. Most targets in
[TARGETS.md](../TARGETS.md) share a large part, such as authentication, a
decoder that turns a JSON result into rows, and the `database/sql` types
that sit on top.

This differs from the other `xo` repositories in one way. There is no
central registry of drivers, and no umbrella package that imports every
driver. `usql` already selects drivers with build tags, and a second list
here makes two copies of one fact.

The repository is one module. D13 keeps the dependencies to the standard
library, `apd` and a few approved binary encodings, so no driver carries a
dependency tree that another driver must not have.

The exported API of the root package is a public API, because a driver
outside this repository can import it too. Keep it small, and export a name
only when a driver in this repository needs it.
