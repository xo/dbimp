package libsql

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
)

// tx is a transaction on a stream of Hrana, which each statement reaches by
// the baton of the last answer (D150).
type tx struct {
	c *conn
	s *stream
	// ctx is the context of BeginTx, because Commit and Rollback take none
	// (D150 and hard rule 4).
	ctx context.Context //nolint:containedctx // Commit and Rollback of driver.Tx take no context (D150).
	// busy is true while a cursor of the transaction reads its rows, and
	// expired once the server rolled the transaction back.
	busy    bool
	expired bool
}

// ensure the interface.
var _ driver.Tx = (*tx)(nil)

// Commit satisfies driver.Tx. It sends COMMIT and closes the stream.
func (t *tx) Commit() error {
	return t.end("COMMIT")
}

// Rollback satisfies driver.Tx. It sends ROLLBACK and closes the stream. A
// transaction whose stream expired is rolled back already (measured).
func (t *tx) Rollback() error {
	if t.expired {
		t.c.tx = nil
		return nil
	}
	return t.end("ROLLBACK")
}

// end sends stmt and close on the stream, and ends the transaction.
func (t *tx) end(stmt string) error {
	defer func() { t.c.tx = nil }()
	if t.expired {
		return fmt.Errorf("ending the transaction with %s: the stream expired, and the server rolled back the transaction", stmt)
	}
	if t.busy {
		return fmt.Errorf("ending the transaction with %s while a statement still reads its rows", stmt)
	}
	_, err := t.c.c.execute(t.ctx, t.s, map[string]any{"sql": stmt}, true)
	if e, ok := errors.AsType[*Error](err); ok && e.Code == CodeStreamExpired {
		return fmt.Errorf("ending the transaction with %s: the stream expired, and the server rolled back the transaction: %w", stmt, err)
	}
	if err != nil {
		return fmt.Errorf("ending the transaction with %s: %w", stmt, err)
	}
	return nil
}
