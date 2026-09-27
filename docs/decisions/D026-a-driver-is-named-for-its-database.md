# D26. A driver is named for its database

Status: Decided.

Ken decided this on 2026-09-27. The package of a driver takes the name of the
database it targets, and never the name of the query language. The
Couchbase driver is `github.com/xo/dbimp/couchbase`, not `n1ql`. A driver
that serves several products takes the name of the product that the others
follow, and its documentation names the others. The name is one short lower
case word, as D6 requires.

The name that the driver registers with `database/sql` is the same name, as
D30 decides.
