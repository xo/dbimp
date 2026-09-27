package surrealdb

import (
	"context"
	"fmt"

	"github.com/xo/dbimp"
)

// Version returns the version of the server, such as "surrealdb-3.3.0",
// through the RPC method version (D57). No statement of SurrealQL returns
// it. dc is a connection of this driver, which sql.Conn.Raw hands over:
//
//	conn, err := db.Conn(ctx)
//	...
//	err = conn.Raw(func(dc any) error {
//		v, err = surrealdb.Version(ctx, dc)
//		return err
//	})
func Version(ctx context.Context, dc any) (string, error) {
	c, ok := dc.(*conn)
	if !ok {
		return "", fmt.Errorf("reading the version through a connection of %T, which is not of this driver: %w", dc, dbimp.ErrNotSupported)
	}
	r, err := c.send(ctx, rpcRequest{Method: "version", Params: []any{}})
	if err != nil {
		return "", fmt.Errorf("reading the version: %w", err)
	}
	defer r.Close()
	if err := r.NextRow(); err != nil {
		return "", fmt.Errorf("reading the version: %w", err)
	}
	v, err := r.value(0)
	if err != nil {
		return "", fmt.Errorf("reading the version: %w", err)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("reading the version: %T is not a string: %w", v, dbimp.ErrInvalidValue)
	}
	return s, nil
}
