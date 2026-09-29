package arangodb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/url"

	"github.com/xo/dbimp"
)

// tx is a stream transaction (D91).
type tx struct {
	c  *conn
	id string
	// database is the database of the transaction, which each of its
	// statements must name, and readonly is true if it names no collection
	// for write.
	database string
	readonly bool
	// ctx is the context of BeginTx, which Commit and Rollback use, as D69
	// does for Neo4j.
	ctx context.Context //nolint:containedctx // D91 keeps the context of BeginTx for Commit and Rollback, which take none.
}

// BeginTx satisfies driver.ConnBeginTx. It names every collection of the
// database that is not a system collection in write, and none for a
// read-only transaction (D91). WithReadonly through WithOptions makes it
// read-only too, and WithDatabase names its database (D109).
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.tx != nil {
		return nil, fmt.Errorf("beginning a transaction: one is open on the connection: %w", dbimp.ErrNotSupported)
	}
	if lvl := sql.IsolationLevel(opts.Isolation); lvl != sql.LevelDefault {
		return nil, fmt.Errorf("beginning a transaction at the isolation %s: %w", lvl, dbimp.ErrNotSupported)
	}
	o, _ := resolve(ctx, &c.c.cfg, c.c.cfg.Database, nil)
	readonly := opts.ReadOnly || o.readonly
	write := []string{}
	if !readonly {
		var list struct {
			Result []struct {
				Name string `json:"name"`
			} `json:"result"`
		}
		if err := c.c.call(ctx, http.MethodGet, api(o.database, "collection?excludeSystem=true"), nil, &list, ""); err != nil {
			return nil, fmt.Errorf("listing the collections of the transaction: %w", err)
		}
		for _, col := range list.Result {
			write = append(write, col.Name)
		}
	}
	var begun struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	body := map[string]any{"collections": map[string]any{"write": write}}
	if err := c.c.call(ctx, http.MethodPost, api(o.database, "transaction/begin"), body, &begun, ""); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	c.tx = &tx{c: c, id: begun.Result.ID, ctx: ctx, database: o.database, readonly: readonly}
	return c.tx, nil
}

// Commit satisfies driver.Tx.
func (t *tx) Commit() error {
	defer func() { t.c.tx = nil }()
	if err := t.c.c.call(t.ctx, http.MethodPut, api(t.database, "transaction/"+url.PathEscape(t.id)), nil, nil, ""); err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}
	return nil
}

// Rollback satisfies driver.Tx. It aborts the transaction with the context of
// BeginTx without its end, and with the limit of a stop, because that context
// can have ended, and a transaction that nobody aborts holds each collection
// until it is idle for 60 seconds (D91).
func (t *tx) Rollback() error {
	defer func() { t.c.tx = nil }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), stopTimeout)
	defer cancel()
	if err := t.c.c.call(ctx, http.MethodDelete, api(t.database, "transaction/"+url.PathEscape(t.id)), nil, nil, ""); err != nil {
		return fmt.Errorf("aborting the transaction: %w", err)
	}
	return nil
}
