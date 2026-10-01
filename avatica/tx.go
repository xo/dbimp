package avatica

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
)

// tx is a transaction of the database behind the server, on a connection
// whose autoCommit is false (D159).
type tx struct {
	c *conn
	// ctx is the context of BeginTx, because Commit and Rollback take none
	// (D159 and hard rule 4).
	ctx context.Context //nolint:containedctx // Commit and Rollback of driver.Tx take no context (D159).
	// props are the properties that BeginTx set, which the end of the
	// transaction sets back.
	props connProps
}

// ensure the interface.
var _ driver.Tx = (*tx)(nil)

// Commit satisfies driver.Tx. It sends commit.
func (t *tx) Commit() error {
	return t.end("commit")
}

// Rollback satisfies driver.Tx. It sends rollback.
func (t *tx) Rollback() error {
	return t.end("rollback")
}

// end sends req, which is commit or rollback, and then connectionSync with
// autoCommit true, and with readOnly and transactionIsolation as they were
// before the transaction (D159).
func (t *tx) end(req string) error {
	defer func() { t.c.tx = nil }()
	_, err := t.c.call(t.ctx, map[string]any{"request": req, "connectionId": t.c.id})
	if err != nil {
		err = fmt.Errorf("ending the transaction with %s: %w", req, err)
	}
	if t.c.bad {
		return err
	}
	p := connProps{AutoCommit: new(true)}
	if t.props.ReadOnly != nil {
		p.ReadOnly = new(t.c.readOnly)
	}
	if t.props.TransactionIsolation != nil {
		p.TransactionIsolation = new(t.c.isolation)
	}
	if _, serr := t.c.sync(t.ctx, p); serr != nil {
		// The connection keeps autoCommit false, so database/sql must not
		// use it again.
		t.c.bad = true
		err = errors.Join(err, fmt.Errorf("ending the transaction: %w", serr))
	}
	return err
}
