package arangodb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// maxQueryText is the length of the text of a query that the list of running
// queries keeps, which is maxQueryStringLength of /_api/query/properties and
// 4096 by default (measured on 3.12.12, D99).
const maxQueryText = 4096

// tag returns a new name for one query of the connection, which the comment
// of tagged holds (D90). Each query has its own, because a connection can
// hold more than one open cursor.
func (c *conn) tag() string {
	c.seq++
	return "dbimp:" + c.id + "-" + strconv.FormatUint(c.seq, 10)
}

// tagged returns query with a line comment that holds tag. The comment goes
// at the end, on a line of its own, where it keeps the positions of an error
// in the text of the caller (D90). A query that would then be longer than
// maxQueryText has it at the start instead, on a line of its own, because the
// list of running queries keeps only the start of a long text (D99). The
// lines of an error in such a query move by one.
func tagged(query, tag string) string {
	if end := query + "\n// " + tag; len(end) <= maxQueryText {
		return end
	}
	return "// " + tag + "\n" + query
}

// isTagged reports whether text, the text of a running query, holds the
// comment of tagged with tag.
func isTagged(text, tag string) bool {
	return strings.HasSuffix(text, "\n// "+tag) || strings.HasPrefix(text, "// "+tag+"\n")
}

// watch kills a query on the server when its context ends before the first
// batch arrives, because a closed connection does not stop it (D90).
type watch struct {
	stop func() bool
	done chan struct{}
	once sync.Once
	ran  bool
	err  error
	// kill kills the query at once, and ended returns the error of the
	// context, or nil while the context lives.
	kill  func() error
	ended func() error
}

// watch returns a watch of ctx for the query that tag names in the database
// db. It returns nil for cancel=none, and every method of a nil watch does
// nothing.
func (c *conn) watch(ctx context.Context, db, tag string) *watch {
	if tag == "" {
		return nil
	}
	w := &watch{done: make(chan struct{}), ended: ctx.Err}
	w.kill = func() error { return c.c.kill(ctx, db, tag) }
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		w.err = w.kill()
	})
	return w
}

// end ends the watch. If the context ended first, it waits until the kill is
// done, and returns true and its error.
func (w *watch) end() (bool, error) {
	if w == nil {
		return false, nil
	}
	w.once.Do(func() {
		switch {
		case !w.stop():
			<-w.done
			w.ran = true
		case w.ended() != nil:
			// The context ended, and the transport saw it before the
			// function of the watch started, so stop kept it from running.
			// The query still runs on the server, and the watch kills it
			// here.
			w.err = w.kill()
			w.ran = true
		}
	})
	return w.ran, w.err
}

// kill finds the running query of the database db whose comment holds tag,
// and kills it (measured). The
// cursor of a streaming query that is killed keeps its collections until the
// next fetch or the end of its ttl (measured). It uses ctx without its end,
// because ctx can have ended.
func (c *Connector) kill(ctx context.Context, db, tag string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	var current []struct {
		ID    string `json:"id"`
		Query string `json:"query"`
	}
	if err := c.call(ctx, http.MethodGet, api(db, "query/current"), nil, &current, ""); err != nil {
		return fmt.Errorf("finding the query to stop it on the server: %w", err)
	}
	var errs []error
	for _, q := range current {
		if !isTagged(q.Query, tag) {
			continue
		}
		if err := c.call(ctx, http.MethodDelete, api(db, "query/"+url.PathEscape(q.ID)), nil, nil, ""); err != nil {
			errs = append(errs, fmt.Errorf("stopping the query %s on the server: %w", q.ID, err))
		}
	}
	return errors.Join(errs...)
}
