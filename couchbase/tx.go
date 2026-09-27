package couchbase

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"

	"github.com/xo/dbimp"
)

// BeginTx satisfies driver.ConnBeginTx. It sends BEGIN WORK, and each later
// statement on the connection carries the txid that it returns, until Commit
// or Rollback ends the transaction (D41).
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if sql.IsolationLevel(opts.Isolation) != sql.LevelDefault {
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	}
	if c.txid != "" {
		return nil, fmt.Errorf("beginning a transaction: one is open: %w", dbimp.ErrNotSupported)
	}
	o := resolve(ctx, &c.c.cfg, nil)
	body := map[string]any{"statement": "BEGIN WORK"}
	if o.durability != "" {
		body["durability_level"] = o.durability
	}
	if o.txTimeout != 0 {
		body["txtimeout"] = o.txTimeout.String()
	}
	r, err := c.send(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	defer r.Close()
	var txid string
	for {
		err := r.NextRow()
		if err != nil {
			if !isEOF(err) {
				return nil, fmt.Errorf("beginning a transaction: %w", err)
			}
			break
		}
		if txid, err = r.txid(); err != nil {
			return nil, fmt.Errorf("reading the txid: %w", err)
		}
	}
	if txid == "" {
		return nil, fmt.Errorf("beginning a transaction: the server returned no txid: %w", dbimp.ErrInvalidValue)
	}
	c.txid, c.txReadonly = txid, opts.ReadOnly
	return &tx{c: c, ctx: ctx}, nil
}

// tx is an open transaction of the query service. It keeps the context of
// BeginTx, which database/sql defines as the lifetime of the transaction,
// because Commit and Rollback take none (D45).
type tx struct {
	c   *conn
	ctx context.Context //nolint:containedctx // D45: database/sql gives Commit and Rollback no context of their own.
}

// Commit satisfies driver.Tx. It sends COMMIT WORK with the context of
// BeginTx.
func (t *tx) Commit() error {
	return t.c.endTx(t.ctx, "COMMIT WORK")
}

// Rollback satisfies driver.Tx. It sends ROLLBACK WORK with the context of
// BeginTx while that context is live. After the context ends, it forgets the
// txid and sends nothing, and the server discards the transaction at its
// timeout, with none of its writes (D45).
func (t *tx) Rollback() error {
	if t.ctx.Err() != nil {
		t.c.txid, t.c.txReadonly = "", false
		return nil //nolint:nilerr // D45: a transaction whose context ended applies nothing, so the rollback is done.
	}
	return t.c.endTx(t.ctx, "ROLLBACK WORK")
}

// endTx sends COMMIT WORK or ROLLBACK WORK with the txid, and forgets the
// txid, whether the server ended the transaction or not. A transaction that
// the server did not end expires at its timeout.
func (c *conn) endTx(ctx context.Context, statement string) error {
	txid := c.txid
	c.txid, c.txReadonly = "", false
	r, err := c.send(ctx, map[string]any{"statement": statement, "txid": txid})
	if err != nil {
		return fmt.Errorf("sending %s: %w", statement, err)
	}
	defer r.Close()
	if err := r.drain(); err != nil {
		return fmt.Errorf("sending %s: %w", statement, err)
	}
	return nil
}

// txid returns the txid in the row of BEGIN WORK. Its signature is "json",
// so the row is one object, but a column named txid is read as well.
func (r *rows) txid() (string, error) {
	if i := r.column("txid"); i >= 0 {
		return dbimp.String(r.vals[i])
	}
	if r.mode != modeRaw || r.vals[0].Kind() != '{' {
		return "", nil
	}
	var v struct {
		TxID string `json:"txid"`
	}
	if err := json.Unmarshal(r.vals[0], &v); err != nil {
		return "", err
	}
	return v.TxID, nil
}
