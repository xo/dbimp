package influxdb

import (
	"context"
	"fmt"

	"github.com/xo/dbimp"
)

// Version returns the release of the server of a connection, from the header
// X-Influxdb-Version of GET /ping, such as 1.13.1, 2.9.1 or 3.11.5 (D78). It
// removes the "v" that InfluxDB 2 writes before the number. A caller calls it
// inside sql.Conn.Raw, with the connection of the driver:
//
//	err = conn.Raw(func(dc any) error {
//		v, err = influxdb.Version(ctx, dc)
//		return err
//	})
func Version(ctx context.Context, dc any) (string, error) {
	c, ok := dc.(*conn)
	if !ok {
		return "", fmt.Errorf("reading the version through a connection of %T, which is not of this driver: %w", dc, dbimp.ErrNotSupported)
	}
	return c.c.ping(ctx)
}

// Dialect returns the dialect that a connection speaks: SQL or InfluxQL
// (D78). A caller calls it inside sql.Conn.Raw, as for Version.
func Dialect(dc any) (string, error) {
	c, ok := dc.(*conn)
	if !ok {
		return "", fmt.Errorf("reading the dialect through a connection of %T, which is not of this driver: %w", dc, dbimp.ErrNotSupported)
	}
	return c.dialect, nil
}
