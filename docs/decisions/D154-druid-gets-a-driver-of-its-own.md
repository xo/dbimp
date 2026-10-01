# D154. Druid gets a driver of its own

Status: Amends D74.

Ken decided this on 2026-10-01, at step 2 of the Avatica driver. Apache
Druid is a target of its own, with a driver that speaks its SQL API,
`POST /druid/v2/sql`, and registers the name `druid` (D26 and D28). It
comes later in the order, as number 23 of [TARGETS.md](../TARGETS.md) has
it.

The Avatica driver serves the standalone Avatica server and the Apache
Phoenix Query Server, and not Druid, although Druid also takes Avatica at
`/druid/v2/sql/avatica/`. D74 named Druid as a flavor of Avatica, and this
decision takes it out.
