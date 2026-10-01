# D153. Avatica speaks JSON only

Status: Decided.

Ken decided this on 2026-10-01, at step 2 of the Avatica driver. This is
item 12 of step 9 of [DRIVER.md](../DRIVER.md) for Avatica.

- The driver speaks the JSON form of the Avatica protocol, and no protobuf.
  So it adds no binary encoding (D13).
- The standalone server that `dbrun` runs answered a body of JSON with an
  `ErrorResponse` in protobuf (measured with `curl` on 2026-10-01), and the
  Phoenix Query Server takes protobuf by default (AVATICA.md). A server of
  Avatica serves JSON when it starts with it. So the `dbmeta` session starts
  each entry with JSON.
- A server that runs protobuf only cannot be reached through this driver.
