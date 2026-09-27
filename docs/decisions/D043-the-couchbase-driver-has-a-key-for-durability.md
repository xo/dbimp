# D43. The Couchbase driver has a key for durability

Status: Amends D38 and D40.

Ken decided this on 2026-09-27, and it amends D38 and D40. The DSN takes the
key `durability_level`, with the values that the server takes: `none`,
`majority`, `majorityAndPersistActive` and `persistToMajority`. If the DSN
has no such key, the driver sends none, and the server uses its default,
which is `majority`. `WithOptions` sets it for one transaction, by the order
of D40.

The survey found that a transaction with the default durability cannot
commit on the one node that `dbrun` starts, and fails with code 17007
([COUCHBASE.md](../COUCHBASE.md)). The integration tests set
`durability_level=none`, and a test of the default expects that refusal.
