package neo4j

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"

	"github.com/xo/dbimp"
)

// BeginTx satisfies driver.ConnBeginTx. It opens an explicit transaction of
// the Query API, and each later statement of the connection goes to it,
// until Commit or Rollback ends it (D65). ReadOnly opens it in read access
// mode.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if sql.IsolationLevel(opts.Isolation) != sql.LevelDefault {
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	}
	if c.tx != nil {
		return nil, fmt.Errorf("beginning a transaction: one is open: %w", dbimp.ErrNotSupported)
	}
	var b body
	if opts.ReadOnly {
		b.AccessMode = "READ"
	}
	meta := false
	if c.c.cfg.Cancel == CancelMetadata {
		ok, err := c.c.takesMetadata(ctx)
		if err != nil {
			return nil, fmt.Errorf("beginning a transaction: %w", err)
		}
		if ok {
			meta, b.TxMetadata = true, c.metadata()
		}
	}
	res, err := c.c.post(ctx, http.MethodPost, c.c.queryPath()+"/tx", b, version10)
	if err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	r, err := readResponse(res, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	defer r.Close()
	if err := r.drain(); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	if r.txID == "" {
		return nil, fmt.Errorf("beginning a transaction: the server returned no id: %w", dbimp.ErrInvalidValue)
	}
	c.tx = &tx{c: c, id: r.txID, ctx: ctx, meta: meta}
	return c.tx, nil
}

// tx is an explicit transaction of the Query API. It keeps the context of
// BeginTx, which database/sql defines as the lifetime of the transaction,
// because Commit and Rollback take none (D69).
type tx struct {
	c   *conn
	id  string
	ctx context.Context //nolint:containedctx // D69: database/sql gives Commit and Rollback no context of their own.
	// meta is true if the transaction carries txMetadata (D67).
	meta bool
	// ended is the error that ended the transaction on the server, or nil.
	ended error
}

// Commit satisfies driver.Tx. If an error ended the transaction on the
// server, it returns that error and sends nothing (D65).
func (t *tx) Commit() error {
	t.c.tx = nil
	if t.ended != nil {
		return fmt.Errorf("committing the transaction: it ended: %w", t.ended)
	}
	res, err := t.c.c.post(t.ctx, http.MethodPost, t.path()+"/commit", body{}, version10)
	if err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}
	r, err := readResponse(res, nil)
	if err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}
	defer r.Close()
	if err := r.drain(); err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}
	return nil
}

// Rollback satisfies driver.Tx. If an error ended the transaction on the
// server, or the context of BeginTx ended, it sends nothing, and the server
// discards the transaction at its idle timeout (D65 and D69).
func (t *tx) Rollback() error {
	t.c.tx = nil
	if t.ended != nil {
		return nil //nolint:nilerr // D65: the error that ended the transaction rolled it back on the server.
	}
	if t.ctx.Err() != nil {
		return nil //nolint:nilerr // D69: a transaction whose context ended applies nothing, so the rollback is done.
	}
	res, err := t.c.c.post(t.ctx, http.MethodDelete, t.path(), body{}, version10)
	if err != nil {
		return fmt.Errorf("rolling back the transaction: %w", err)
	}
	if res.StatusCode >= http.StatusMultipleChoices {
		_, err := readResponse(res, nil)
		return fmt.Errorf("rolling back the transaction: %w", err)
	}
	// A rollback answers HTTP 200 with an empty body on 5.26.31, and with
	// {} on 2026.09.0 (measured).
	s := dbimp.NewStream(res.Body)
	err = readEmpty(s)
	if cerr := s.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("rolling back the transaction: %w", err)
	}
	return nil
}

// path returns the path of the transaction.
func (t *tx) path() string {
	return t.c.c.queryPath() + "/tx/" + t.id
}

// readEmpty reads a body that is empty or holds one value that means
// nothing, such as {}.
func readEmpty(s *dbimp.Stream) error {
	if s.Decoder().PeekKind() != 0 {
		if err := s.Decoder().SkipValue(); err != nil {
			return err
		}
	}
	return s.End()
}
