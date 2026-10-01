# D161. A driver tests each product that it recorded

Status: Decided.

Ken decided this on 2026-10-01, after step 14 of the Avatica driver. He
answered open question 3 of [AVATICA.md](../AVATICA.md).

- The workflow tests each release of the product that has the name of a
  driver, as before (D30). It also tests each release of a product whose
  release the manifest of a driver recorded, under that driver. So
  `phoenix-2.0-5.0`, whose product in `dbmeta` is `phoenix`, runs the tests
  of `avatica`.
- The workflow derives this map from `testdata/<driver>/manifest.json` and
  the list of `dbrun`, so it holds no list of products or of drivers.
- The prefix of the variables of the environment is the name of the driver,
  such as `AVATICA_DSN`, and not the name of the product.
- `dbmeta` keeps the product `phoenix`. Renaming it to `avatica` would lose
  the name of the product there.
