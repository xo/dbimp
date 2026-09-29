package neo4j

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the requests that stop a statement on the server after
// its context ended (D67).
const stopTimeout = 5 * time.Second

// The statements that find the transaction of a statement on the server, for
// cancel=tag and cancel=metadata (D67). The tag ends the statement, so
// cancel=tag finds it with ENDS WITH (D95). The ordinary user sees its own
// transactions, and can terminate them (measured).
const (
	showByTag      = "SHOW TRANSACTIONS YIELD transactionId, currentQuery WHERE currentQuery ENDS WITH $tag RETURN transactionId"
	showByMetadata = "SHOW TRANSACTIONS YIELD transactionId, metaData WHERE metaData.dbimp = $id RETURN transactionId"
	terminate      = "TERMINATE TRANSACTION $id"
)

// watch stops a statement on the server when its context ends before end
// runs. The server does not stop a statement when the client disconnects
// (D67).
type watch struct {
	stop func() bool
	done chan struct{}
	once sync.Once
	ran  bool
	err  error
	// cause is the cause of the end of the context, once the stop ran.
	cause error
	// now stops the statement on the server at once, for an early Close
	// (D105).
	now func() error
}

// watch returns a watch of ctx for a statement of the connection, which
// finds the statement as how says. It returns nil if how is "", and every
// method of a nil watch does nothing.
func (c *conn) watch(ctx context.Context, how string) *watch {
	if how == "" {
		return nil
	}
	w := &watch{done: make(chan struct{})}
	w.now = func() error { return c.stopStatement(ctx, how) }
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		w.cause = context.Cause(ctx)
		w.err = c.stopStatement(ctx, how)
	})
	return w
}

// abandon ends the watch, and stops the statement on the server at once,
// because the caller closed its rows before their end (D105). If the context
// ended first, the watch stops the statement itself, and abandon waits for
// it.
func (w *watch) abandon() error {
	if w == nil {
		return nil
	}
	var err error
	w.once.Do(func() {
		if !w.stop() {
			<-w.done
			w.ran = true
			return
		}
		err = w.now()
	})
	return err
}

// end ends the watch. If the context ended first, it waits until the stop of
// the statement is done, and returns true and the error of the stop.
func (w *watch) end() (bool, error) {
	if w == nil {
		return false, nil
	}
	w.once.Do(func() {
		if !w.stop() {
			<-w.done
			w.ran = true
		}
	})
	return w.ran, w.err
}

// stopStatement finds the transaction of the running statement of the
// connection, and terminates it. It uses ctx without its end, because ctx
// ended, and stopTimeout bounds it.
func (c *conn) stopStatement(ctx context.Context, how string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	show, name, value := showByTag, "tag", c.tag()
	if how == CancelMetadata {
		show, name, value = showByMetadata, "id", c.id
	}
	ids, err := c.c.column(ctx, show, name, value)
	if err != nil {
		return fmt.Errorf("finding the statement to stop it on the server: %w", err)
	}
	var errs []error
	for _, id := range ids {
		if _, err := c.c.column(ctx, terminate, "id", id); err != nil {
			errs = append(errs, fmt.Errorf("stopping the transaction %s on the server: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// column runs statement, with the one string argument value named name, and
// returns the strings of its first column. It sends no comment and no
// txMetadata, so that a stop never finds itself.
func (c *Connector) column(ctx context.Context, statement, name, value string) ([]string, error) {
	arg, _, err := encode(value)
	if err != nil {
		return nil, err
	}
	b := body{Statement: statement, Parameters: map[string]jsontext.Value{name: arg}}
	res, err := c.post(ctx, http.MethodPost, c.queryPath(c.cfg.Database), b, version10)
	if err != nil {
		return nil, err
	}
	r, err := readResponse(res, nil)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out []string
	for {
		err := r.NextRow()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(r.vals) == 0 {
			continue
		}
		v, err := decode(r.vals[0])
		if err != nil {
			return nil, err
		}
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
}

// newRelease reports whether the release of the server is 2026.04 or later.
// Such a release takes txMetadata (D67), and honors maxExecutionTime, which
// 5.26.31 ignores (measured on 5.26.31 and 2026.09.0, D109). It reads the
// release from GET / once for the connector.
func (c *Connector) newRelease(ctx context.Context) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metadata != nil {
		return *c.metadata, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/", nil)
	if err != nil {
		return false, fmt.Errorf("making the request for the release: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return false, fmt.Errorf("reading the release: %w", err)
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return false, fmt.Errorf("reading the release: %w", err)
	}
	defer res.Body.Close()
	var d struct {
		Version string `json:"neo4j_version"`
	}
	if err := json.UnmarshalRead(res.Body, &d); err != nil {
		return false, fmt.Errorf("reading the release: %w", err)
	}
	ok := calendarAtLeast(d.Version, 2026, 4)
	c.metadata = &ok
	return ok, nil
}

// calendarAtLeast reports whether the release version, such as 2026.09.0, is
// the release year.month or later. A release of the old form, such as
// 5.26.31, is older than every release of the calendar form.
func calendarAtLeast(version string, year, month int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return y > year || y == year && m >= month
}
