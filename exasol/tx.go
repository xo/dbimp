package exasol

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
)

// tx is a transaction. It lasts from BeginTx, which turns autocommit off, to
// Commit or Rollback, which send COMMIT or ROLLBACK and turn autocommit on
// again (D209).
type tx struct {
	c *conn
	// ctx is the context of BeginTx, because Commit and Rollback of driver.Tx
	// take none (D209 and hard rule 4).
	ctx context.Context //nolint:containedctx // Commit and Rollback of driver.Tx take no context (D209).
	// restore is true if autocommit was on before the transaction, so the end
	// turns it on.
	restore bool
}

// ensure the interface.
var _ driver.Tx = (*tx)(nil)

// Commit satisfies driver.Tx.
func (t *tx) Commit() error {
	return t.end("COMMIT")
}

// Rollback satisfies driver.Tx.
func (t *tx) Rollback() error {
	return t.end("ROLLBACK")
}

// end sends stmt, and turns autocommit on if the transaction turned it off.
// database/sql rolls back a transaction whose context ended, and that context
// stops the request before it reaches the server, so the end takes the
// context without its end, and the limit of a close (D209, as D150 does for
// libSQL).
func (t *tx) end(stmt string) error {
	defer func() { t.c.tx = nil }()
	ctx, cancel := closeContext(t.ctx)
	defer cancel()
	_, err := t.c.s.call(ctx, executeRequest{Command: cmdExecute, SQLText: stmt})
	if err != nil {
		err = fmt.Errorf("ending the transaction with %s: %w", stmt, err)
	}
	if t.restore && t.c.s.failure() == nil {
		// A failed statement leaves autocommit off, which turns the next
		// statement of the connection into a transaction that nobody ends.
		if rerr := t.c.setAutocommit(ctx, true); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}
	return err
}
