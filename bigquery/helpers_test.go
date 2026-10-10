package bigquery //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// testProject is the project of the hosted recordings. The recorder replaced the
// real id with this one in every file.
const testProject = "dbimp-project"

// emulatorProject is the project that dbrun makes in the emulator.
const emulatorProject = "dbmeta"

// config returns the configuration of the tests: the hosted project, with no
// login, because a fake server checks none.
func config() Config {
	return Config{Project: testProject, DisableAuth: true}
}

// fastPoll is the interval of the poll of the tests, so that no test waits a
// fixed time.
const fastPoll = time.Millisecond

// connector returns a connector for cfg that talks to the fake server at base.
func connector(cfg Config, base string) *Connector {
	c := NewConnector(cfg)
	c.base = base
	c.pollMin, c.pollMax = fastPoll, 4*fastPoll
	return c
}

// open returns a database on the fake server at base, with the connector of
// cfg.
func open(t *testing.T, cfg Config, base string) *sql.DB {
	t.Helper()
	return openConnector(t, connector(cfg, base))
}

// openConnector returns a database on the connector c, and closes both when the
// test ends.
func openConnector(t *testing.T, c *Connector) *sql.DB {
	t.Helper()
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// errAs returns err as a *Error.
func errAs(err error) (*Error, bool) {
	var e *Error
	return e, errors.As(err, &e)
}

// isIncomplete reports whether err wraps dbimp.ErrIncomplete.
func isIncomplete(err error) bool { return errors.Is(err, dbimp.ErrIncomplete) }

// isNotSupported reports whether err wraps dbimp.ErrNotSupported.
func isNotSupported(err error) bool { return errors.Is(err, dbimp.ErrNotSupported) }

// isDeadline reports whether err wraps context.DeadlineExceeded.
func isDeadline(err error) bool { return errors.Is(err, context.DeadlineExceeded) }

// isCanceled reports whether err is ErrCanceled.
func isCanceled(err error) bool { return errors.Is(err, ErrCanceled) }
