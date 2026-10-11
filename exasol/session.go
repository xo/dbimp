package exasol

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// The bounds of the requests that the driver sends without a context of the
// caller, and of the wait for the answer of an aborted statement.
const (
	// closeTimeout bounds a request that frees something on the server, such
	// as a result set, after the context of the caller ended (D210).
	closeTimeout = 5 * time.Second
	// abortWrite bounds the write of abortQuery.
	abortWrite = 5 * time.Second
	// abortGrace is how long the driver waits for the answer of an aborted
	// statement. If it does not come, the driver closes the session, so a hung
	// server cannot hang the caller (D211).
	abortGrace = 10 * time.Second
	// defaultMaxData is the largest answer when the login names none, as the
	// root client reads it.
	defaultMaxData = dbimp.DefaultMaxMessage
)

// abortFrame is the frame that asks the server to stop the running statement.
// It has no answer of its own: the statement answers with the code R0003
// (recorded: "abort a running query").
const abortFrame = `{"command":"abortQuery"}`

// session is one WebSocket to the server, after its login. The server runs one
// request at a time on a session, so exchange holds a lock from the write of a
// request to the end of its answer.
type session struct {
	ws       *dbimp.WebSocket
	compress bool
	maxData  int64
	// grace is how long exchange waits for the answer of an aborted statement.
	// It is zero, which means abortGrace, except in a test.
	grace time.Duration

	mu sync.Mutex

	// stateMu guards the fields below, which a statement changes while
	// another goroutine can read them.
	stateMu    sync.Mutex
	err        error
	tz         string
	loc        *time.Location
	locName    string
	schema     string
	autocommit bool
}

// failure returns the error that ended the session, or nil.
func (s *session) failure() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.err
}

// fail ends the session with err, once. It closes the socket, because the
// stream of frames cannot be mended after a fault.
func (s *session) fail(err error) {
	s.stateMu.Lock()
	first := s.err == nil
	if first {
		s.err = err
	}
	s.stateMu.Unlock()
	if first {
		_ = s.ws.Close(dbimp.CloseNormal, "")
	}
}

// broken is the error for a session that ended.
func (s *session) broken() error {
	return fmt.Errorf("the session ended: %w: %w", driver.ErrBadConn, s.failure())
}

// exchange sends body as one frame, and reads the one frame that answers it.
// If the context ends while the server runs the statement, it sends
// abortQuery, reads the answer of the aborted statement, and keeps the session
// (D211). The error is then the error of the context. An answer with the status
// error is an *Error.
//
// A session that ended before the write gives driver.ErrBadConn, and so does a
// write that failed, because the statement did not reach the server. A failure
// after the write never does (D8).
func (s *session) exchange(ctx context.Context, body []byte) (*envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure() != nil {
		return nil, s.broken()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending a request: %w", err)
	}
	if err := s.write(ctx, body); err != nil {
		s.fail(err)
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("sending a request: %w", cerr)
		}
		return nil, fmt.Errorf("sending a request: %w: %w", driver.ErrBadConn, err)
	}
	ab := &aborter{s: s, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { ab.run(ctx) })
	// The read takes no part in the cancel of the caller. The abort makes the
	// server answer, and the answer ends the read (D211).
	mt, msg, err := s.ws.Read(context.WithoutCancel(ctx))
	if !stop() {
		<-ab.done
	}
	ab.stop()
	if err != nil {
		s.fail(err)
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("reading an answer: %w: %w", cerr, err)
		}
		return nil, fmt.Errorf("reading an answer: %w", err)
	}
	env, err := s.decode(mt, msg)
	if env != nil {
		s.apply(env.Attributes)
	}
	var serr *Error
	switch {
	case err == nil:
		return env, nil
	case errors.As(err, &serr):
		if cerr := ctx.Err(); cerr != nil && (serr.Code == CodeAborted || serr.Code == CodeTimeout) {
			return env, fmt.Errorf("running the statement: %w: %w", cerr, serr)
		}
		return env, err
	}
	s.fail(err)
	return nil, err
}

// write sends one request frame, compressed if the session is.
func (s *session) write(ctx context.Context, body []byte) error {
	if !s.compress {
		return s.ws.Write(ctx, dbimp.MessageText, body)
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(body); err != nil {
		return fmt.Errorf("compressing a request: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("compressing a request: %w", err)
	}
	return s.ws.Write(ctx, dbimp.MessageBinary, buf.Bytes())
}

// decode reads an answer. A binary frame is zlib data, and a text frame is
// JSON, whatever the session asked for (recorded: "a text frame after
// compression was turned on").
func (s *session) decode(mt dbimp.MessageType, msg []byte) (*envelope, error) {
	if mt == dbimp.MessageBinary {
		limit := s.maxData
		if limit <= 0 {
			limit = defaultMaxData
		}
		zr, err := zlib.NewReader(bytes.NewReader(msg))
		if err != nil {
			return nil, fmt.Errorf("reading a compressed answer: %w", err)
		}
		defer zr.Close()
		var buf bytes.Buffer
		n, err := buf.ReadFrom(io.LimitReader(zr, 2*limit+1))
		if err != nil {
			return nil, fmt.Errorf("reading a compressed answer: %w", err)
		}
		if n > 2*limit {
			return nil, fmt.Errorf("reading a compressed answer: more than %d bytes: %w", 2*limit, dbimp.ErrMessageTooBig)
		}
		msg = buf.Bytes()
	}
	return decodeEnvelope(msg)
}

// apply takes the attributes of the session that an answer reports.
func (s *session) apply(a *attributes) {
	if a == nil {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if a.Timezone != nil {
		s.tz = *a.Timezone
	}
	if a.Autocommit != nil {
		s.autocommit = *a.Autocommit
	}
	if a.CurrentSchema != nil {
		s.schema = *a.CurrentSchema
	}
}

// aborter sends abortQuery when the context of an exchange ends, and closes the
// session if the answer of the statement does not follow.
type aborter struct {
	s     *session
	done  chan struct{}
	mu    sync.Mutex
	timer *time.Timer
	over  bool
}

// run is the function that runs when the context ends.
func (a *aborter) run(ctx context.Context) {
	defer close(a.done)
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortWrite)
	defer cancel()
	// The write goes out of turn with the read that waits, which the socket
	// allows, because one goroutine can write while another reads. A failed
	// write closes the socket, and the read then fails too.
	// With compression on, the server refuses a text frame, even to abort a
	// statement: it answers with an error of its own and the session is lost
	// (measured on 2026-10-11, and not recorded). So the abort is compressed too.
	if err := a.s.write(wctx, []byte(abortFrame)); err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.over {
		grace := a.s.grace
		if grace == 0 {
			grace = abortGrace
		}
		a.timer = time.AfterFunc(grace, func() {
			a.s.fail(fmt.Errorf("waiting for the answer of an aborted statement: %w", context.DeadlineExceeded))
		})
	}
}

// stop ends the wait for the answer of an aborted statement.
func (a *aborter) stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.over = true
	if a.timer != nil {
		a.timer.Stop()
	}
}

// command sends the request v, with the members of params in place of its own
// (D109), and returns the answer.
func (s *session) command(ctx context.Context, v any, params map[string]any) (*envelope, error) {
	body, err := dbimp.MarshalParams(v, params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return s.exchange(ctx, body)
}

// call sends the request v.
func (s *session) call(ctx context.Context, v any) (*envelope, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return s.exchange(ctx, body)
}

// location returns the time zone of the session, which a statement changes
// with ALTER SESSION SET TIME_ZONE. The server writes the name in upper case,
// such as ASIA/TOKYO (recorded: "set the time zone to Tokyo").
func (s *session) location() (*time.Location, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.loc != nil && s.locName == s.tz {
		return s.loc, nil
	}
	loc, err := loadZone(s.tz)
	if err != nil {
		return nil, err
	}
	s.loc, s.locName = loc, s.tz
	return loc, nil
}

// loadZone returns the location that the server names. The server writes a name
// in upper case, so the function tries the name as it is, then each way that
// the database of zones spells a name.
func loadZone(name string) (*time.Location, error) {
	if name == "" {
		return nil, fmt.Errorf("loading the time zone of the session: the server named none: %w", dbimp.ErrInvalidValue)
	}
	for _, try := range zoneSpellings(name) {
		if loc, err := time.LoadLocation(try); err == nil {
			return loc, nil
		}
	}
	return nil, fmt.Errorf("loading the time zone %q of the session: %w", name, dbimp.ErrInvalidValue)
}

// zoneSpellings returns the names to try for the zone name, such as Asia/Tokyo
// for ASIA/TOKYO, America/New_York for AMERICA/NEW_YORK and Etc/GMT+5 for
// ETC/GMT+5.
func zoneSpellings(name string) []string {
	parts := strings.Split(name, "/")
	title := make([]string, len(parts))
	for i, p := range parts {
		title[i] = titleZone(p)
	}
	spellings := []string{name, strings.Join(title, "/")}
	if len(parts) > 1 {
		upperFirst := append([]string{strings.ToUpper(parts[0])}, title[1:]...)
		upperRest := append([]string{title[0]}, parts[1:]...)
		spellings = append(spellings, strings.Join(upperFirst, "/"), strings.Join(upperRest, "/"))
	}
	return spellings
}

// titleZone writes each word of a part of a zone name with a capital, where a
// word starts after an underscore or a hyphen.
func titleZone(part string) string {
	b := []byte(strings.ToLower(part))
	for i := range b {
		if (i == 0 || b[i-1] == '_' || b[i-1] == '-') && b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}
