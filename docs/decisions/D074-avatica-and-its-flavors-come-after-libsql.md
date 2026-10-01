# D74. Avatica and its flavors come after libSQL

Status: Amends D73, and amended by D88, D127 and D154.

Ken decided on 2026-09-27 to add Apache Calcite Avatica and its variants
to the order of D73, after libSQL. So the order after Neo4j is InfluxDB,
CrateDB, ArangoDB, Databend, TDengine, Apache Pinot, rqlite, libSQL, and
then Avatica.

Avatica is a wire protocol, and not a database. It carries JDBC calls over
HTTP, in JSON or protobuf, to a server that runs the statement on a
database behind it (not measured, from the JSON reference of Avatica). So
one driver serves each product that speaks it, and each such product is a
flavor by "Flavors" in [DRIVER.md](../DRIVER.md). The flavors known on
2026-09-27 are these:

1. The standalone Avatica server, which the image
   `apache/calcite-avatica-hypersql` runs in front of HSQLDB.
2. The Apache Phoenix Query Server, in front of Apache Phoenix on HBase.
3. Apache Druid, whose Broker and Router take Avatica at
   `/druid/v2/sql/avatica/`. Druid is also a target of its own in
   [TARGETS.md](../TARGETS.md), with its own SQL API.

Which flavors the driver serves, and whether each one passes R, is a
question of steps 2 and 4 of the driver, and not of this decision.
[AVATICA.md](../AVATICA.md) holds what is known.

`usql` and `dburl` have an Avatica driver now, `apache/calcite-avatica-go`,
and a scheme `avatica` with the alias `phoenix`. A driver here can replace
it (D24).
