package databend

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// The states of a transaction in the session (measured).
const (
	txActive = "Active"
	txFail   = "Fail"
)

// The errors of a transaction that ended on the server before its Commit
// (D121 and D122).
const (
	errTxCommitted dbimp.Error = "a statement committed the transaction, as a DDL statement does"
	errTxFailed    dbimp.Error = "a statement of the transaction failed"
)

// tx is a transaction, which the session of the connection carries (D121).
type tx struct {
	c *conn
	// ctx is the context of BeginTx, which database/sql defines as the
	// lifetime of the transaction, because Commit and Rollback take none
	// (D122, as D69 does for Neo4j).
	ctx context.Context //nolint:containedctx // D122: database/sql gives Commit and Rollback no context of their own.
	// ended is the reason that the transaction ended on the server, or nil.
	// failed is true when an error ended it, and the server needs ROLLBACK.
	ended  error
	failed bool
}

// BeginTx satisfies driver.ConnBeginTx. It sends BEGIN, and each later
// statement of the connection runs in the transaction, until Commit or
// Rollback ends it (D121).
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	switch {
	case sql.IsolationLevel(opts.Isolation) != sql.LevelDefault:
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	case opts.ReadOnly:
		return nil, fmt.Errorf("beginning a read-only transaction: %w", dbimp.ErrNotSupported)
	case c.tx != nil:
		return nil, fmt.Errorf("beginning a transaction: one is open: %w", dbimp.ErrNotSupported)
	}
	if _, err := c.exec(ctx, "BEGIN", nil); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	if st := c.current().str("txn_state"); st != txActive {
		return nil, fmt.Errorf("beginning a transaction: the server answered the state %q: %w", st, dbimp.ErrInvalidValue)
	}
	c.tx = &tx{c: c, ctx: ctx}
	return c.tx, nil
}

// Commit satisfies driver.Tx. If the transaction ended on the server, it
// returns why, and sends COMMIT only to end a transaction that failed, which
// the server keeps until then (D121 and D122).
func (t *tx) Commit() error {
	// The transaction leaves the connection first, so that the statement
	// that ends it runs, even after the transaction ended.
	t.c.tx = nil
	if t.ended != nil {
		if t.failed {
			_, _ = t.c.exec(context.WithoutCancel(t.ctx), "ROLLBACK", nil)
		}
		return fmt.Errorf("committing the transaction: %w", t.ended)
	}
	if _, err := t.c.exec(t.ctx, "COMMIT", nil); err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}
	return nil
}

// Rollback satisfies driver.Tx. It sends ROLLBACK with the context of
// BeginTx without its end, and with the limit of a stop, because that context
// can have ended (D122, as D100 does for Neo4j). A transaction that a
// statement committed has nothing to roll back.
func (t *tx) Rollback() error {
	t.c.tx = nil
	if t.ended != nil && !t.failed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), stopTimeout)
	defer cancel()
	if _, err := t.c.exec(ctx, "ROLLBACK", nil); err != nil {
		return fmt.Errorf("rolling back the transaction: %w", err)
	}
	return nil
}
