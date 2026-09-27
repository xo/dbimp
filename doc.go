// Package dbimp holds the code that the dbimp database/sql drivers share,
// such as the HTTP client and the adapters that encode and decode values.
//
// Each driver is its own package under this module, such as
// github.com/xo/dbimp/<driver>. A driver registers itself from init under
// the name that github.com/xo/dburl gives its scheme, and a consumer imports
// the driver package directly. There is no registry of drivers.
package dbimp
