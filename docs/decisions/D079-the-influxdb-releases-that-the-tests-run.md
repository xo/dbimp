# D79. The InfluxDB releases that the tests run

Status: Decided.

Ken decided on 2026-09-28 which releases of InfluxDB the tests of the
driver of D78 run. Each release is an image of `docker.io/library/influxdb`
that is still rebuilt (Docker Hub, read on 2026-09-28):

| Release | Tier | The dialects that the tests run |
| --- | --- | --- |
| 1.13.1 | Tested | `influxql` |
| 2.9.1 | Tested | `influxql` |
| 3.9.13 | Tested | `influxql` and `influxdb` |
| 3.11.5 | Tested | `influxql` and `influxdb` |
| 1.11.8 | Nightly | `influxql` |
| 2.8.0 | Nightly | `influxql` |
| 3.10.6 | Nightly | `influxql` and `influxdb` |

A push runs the Tested tier, and the nightly run adds the Nightly tier.
Together the two tiers run the floor and the ceiling of each major release,
as step 2 of the evaluation of `dbmeta` asks, because the floor of InfluxDB
1 and InfluxDB 2 is in the Nightly tier.

Gemini and DeepSeek were asked on 2026-09-28. Gemini said that the API of
InfluxDB 1 and InfluxDB 2 is frozen in maintenance, so the newest of each
serves a push, and that InfluxDB 3 changes fast, so its floor and its
ceiling both run on a push. DeepSeek said that every floor and ceiling runs
on a push. Ken chose Gemini's form. The `dbrun` entry of InfluxDB 3 Core
already has 3.9.13 and 3.11.5 as Tested and 3.10.6 as Nightly
(dbmeta D112). InfluxDB 1 and InfluxDB 2 need entries of their own.

Note of 2026-09-29: dbmeta D114 added the entries of InfluxDB 1 and
InfluxDB 2, with 1.13.1 and 2.9.1 in the tier Tested and 1.11.8 and 2.8.0 in
the tier Nightly.

Note of 2026-09-29, on the tiers: dbmeta D119 put every release that no
model of `dbmeta` reads in the tier Staged. Tested and Nightly above are now
the cadence of each Staged release (dbmeta D120), and CI selects a release
by that cadence.
