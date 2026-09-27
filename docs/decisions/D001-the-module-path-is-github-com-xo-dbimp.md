# D1. The module path is github.com/xo/dbimp

Status: Decided.

Every `xo` module is named `github.com/xo/<repository>`. A driver is a
package inside this module, so its import path is
`github.com/xo/dbimp/<driver>` (D4). The root package `dbimp` holds
the code that the drivers share.
