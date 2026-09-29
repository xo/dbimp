# D106. An ArangoDB geo index on one field reads GeoJSON

Status: Decided.

Ken decided this on 2026-09-29. `CREATE GEO INDEX <name> ON <collection>
(<field>)` of D92 sends `geoJson: true` for an index on one field. The server
takes `false` by default. This was measured on 3.12.12 on 2026-09-29, with
the same three documents in each index:

- With `false`, the index reads an array as `[latitude, longitude]`, and it
  does not index a GeoJSON object: a query through the index did not find
  `{type: "Point", coordinates: [13.4, 52.5]}`.
- With `true`, the index reads an array as `[longitude, latitude]`, and it
  indexes a GeoJSON object.

`true` is the only setting that indexes GeoJSON, and its order is the order
of GeoJSON and of the `GEO_*` functions of AQL. A caller who stores
`[latitude, longitude]` in one field gets wrong answers from such an index,
and stores them in two fields in place of one, with the latitude first:
`CREATE GEO INDEX g ON c (lat, lon)` (the HTTP manual, not measured).
