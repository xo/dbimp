package dbimptest_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// memStore is a table of one column in memory, for the tests of RoundTrip.
// It understands five statements: INSERT, LITERAL <key> <int>, SELECT,
// UPDATE and DELETE.
type memStore struct {
	mu   sync.Mutex
	rows map[string]any
	// lag makes a write visible to a read only after it.
	lag time.Duration
	// seen is when each key last changed.
	seen map[string]time.Time
	// stringify returns each value as a string, which is a fault.
	stringify bool
	// keep makes DELETE do nothing, which is a fault.
	keep bool
	// keepOnNull makes an UPDATE to NULL keep the old value, as InfluxDB
	// does for a field that a write leaves out.
	keepOnNull bool
	// dropped is set by the statement DROP of a teardown.
	dropped bool
}

func (s *memStore) write(key string, v any, deleted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case deleted && s.keep:
	case deleted:
		delete(s.rows, key)
	default:
		s.rows[key] = v
	}
	s.seen[key] = time.Now()
}

func (s *memStore) read(key string) (any, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.seen[key]) < s.lag {
		return nil, false, false
	}
	v, ok := s.rows[key]
	if ok && s.stringify && v != nil {
		v = fmt.Sprint(v)
	}
	return v, ok, true
}

type memConnector struct{ s *memStore }

func (c memConnector) Connect(context.Context) (driver.Conn, error) { return memConn(c), nil }
func (c memConnector) Driver() driver.Driver                        { return nil }

type memConn struct{ s *memStore }

func (c memConn) Prepare(string) (driver.Stmt, error) { return nil, dbimp.ErrNotSupported }
func (c memConn) Close() error                        { return nil }
func (c memConn) Begin() (driver.Tx, error)           { return nil, dbimp.ErrNotSupported }

// arg returns the argument with the name name, if the arguments have names,
// and argument i otherwise.
func arg(args []driver.NamedValue, i int, name string) (driver.NamedValue, error) {
	if len(args) > 0 && args[0].Name != "" {
		for _, a := range args {
			if a.Name == name {
				return a, nil
			}
		}
		return driver.NamedValue{}, fmt.Errorf("the store needs the argument %s", name)
	}
	if i >= len(args) {
		return driver.NamedValue{}, fmt.Errorf("the store needs argument %d", i)
	}
	return args[i], nil
}

// keyArg returns argument i, or the argument named key, as a key.
func keyArg(args []driver.NamedValue, i int) (string, error) {
	a, err := arg(args, i, "key")
	if err != nil {
		return "", err
	}
	key, ok := a.Value.(string)
	if !ok {
		return "", fmt.Errorf("the key is a %T, want a string", a.Value)
	}
	return key, nil
}

func (c memConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case query == "DROP":
		c.s.mu.Lock()
		c.s.dropped = true
		c.s.mu.Unlock()
	case query == "INSERT":
		key, err := keyArg(args, 0)
		if err != nil {
			return nil, err
		}
		v, err := arg(args, 1, "value")
		if err != nil {
			return nil, err
		}
		c.s.write(key, v.Value, false)
	case strings.HasPrefix(query, "LITERAL "):
		f := strings.Fields(query)
		v, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return nil, err
		}
		c.s.write(f[1], v, false)
	case query == "UPDATE":
		key, err := keyArg(args, 1)
		if err != nil {
			return nil, err
		}
		v, err := arg(args, 0, "value")
		if err != nil {
			return nil, err
		}
		if v.Value == nil && c.s.keepOnNull {
			break
		}
		c.s.write(key, v.Value, false)
	case query == "DELETE":
		key, err := keyArg(args, 0)
		if err != nil {
			return nil, err
		}
		c.s.write(key, nil, true)
	default:
		return nil, fmt.Errorf("the store does not know %q", query)
	}
	return driver.RowsAffected(1), nil
}

// QueryContext answers SELECT with the value, and SELECT WIDE with the key
// and then the value, as InfluxQL returns other columns with a value.
func (c memConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	key, err := keyArg(args, 0)
	if err != nil {
		return nil, err
	}
	v, ok, visible := c.s.read(key)
	r := &memRows{v: v, ok: ok && visible}
	if query == "SELECT WIDE" {
		r.key = key
	}
	return r, nil
}

type memRows struct {
	v    any
	ok   bool
	done bool
	// key is the key, which a wide row holds before the value.
	key string
}

func (r *memRows) Columns() []string {
	if r.key != "" {
		return []string{"key", "v"}
	}
	return []string{"v"}
}

func (r *memRows) Close() error { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if !r.ok || r.done {
		return io.EOF
	}
	r.done = true
	if r.key != "" {
		dest[0], dest[1] = r.key, r.v
		return nil
	}
	dest[0] = r.v
	return nil
}

func memCase() dbimptest.RoundTripCase {
	return dbimptest.RoundTripCase{
		Type:   "integer",
		Insert: "INSERT",
		Literal: func(key string, v any) (string, error) {
			i, ok := v.(int64)
			if !ok {
				return "", fmt.Errorf("writing %T: %w", v, dbimp.ErrNotSupported)
			}
			return fmt.Sprintf("LITERAL %s %d", key, i), nil
		},
		Select: "SELECT",
		Update: "UPDATE",
		Delete: "DELETE",
		Values: []dbimptest.Value{
			{Name: "one", In: int64(1)},
			{Name: "null", In: nil},
			{Name: "max", In: int64(9223372036854775807)},
		},
	}
}

func openMem(s *memStore) *sql.DB {
	s.rows, s.seen = map[string]any{}, map[string]time.Time{}
	return sql.OpenDB(memConnector{s: s})
}

// recordTB records the failures of a test rather than failing it, so that a
// test can make sure that RoundTrip fails when it must.
type recordTB struct {
	testing.TB

	mu   sync.Mutex
	errs []string
}

func (r *recordTB) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *recordTB) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

func (r *recordTB) Logf(string, ...any) {}

// failures runs f with a recordTB, and returns what failed.
func failures(t *testing.T, f func(tb testing.TB)) []string {
	t.Helper()
	r := &recordTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	return r.errs
}

func TestRoundTripPassesAStoreThatKeepsValues(t *testing.T) {
	db := openMem(&memStore{})
	defer db.Close()
	dbimptest.RoundTrip(t, db, memCase())
}

// A database with named parameters only gets the key and the value by name,
// and the key in the form of KeyArg.
func TestRoundTripNamed(t *testing.T) {
	db := openMem(&memStore{})
	defer db.Close()
	c := memCase()
	c.Named = true
	c.KeyArg = func(key string) any { return "record:" + key }
	literal := c.Literal
	c.Literal = func(key string, v any) (string, error) { return literal("record:"+key, v) }
	dbimptest.RoundTrip(t, db, c)
}

func TestRoundTripWaitsForALateWrite(t *testing.T) {
	db := openMem(&memStore{lag: 120 * time.Millisecond})
	defer db.Close()
	c := memCase()
	c.Wait = 2 * time.Second
	dbimptest.RoundTrip(t, db, c)
}

// The context of a test ends before its cleanup runs, and the first version
// sent the teardown with it, so the teardown never ran.
func TestRoundTripTearsDown(t *testing.T) {
	s := &memStore{}
	db := openMem(s)
	defer db.Close()
	t.Run("round trip", func(t *testing.T) {
		c := memCase()
		c.Teardown = []string{"DROP"}
		dbimptest.RoundTrip(t, db, c)
	})
	if !s.dropped {
		t.Error("the teardown did not run")
	}
}

func TestRoundTripCatchesAFault(t *testing.T) {
	for _, tt := range []struct {
		name  string
		store *memStore
		want  string
	}{
		{"a value that comes back as a string", &memStore{stringify: true}, `read "1" (string), want 1 (int64)`},
		{"a delete that keeps the row", &memStore{keep: true}, "the row is still there"},
		{"a write that is late, with no wait", &memStore{lag: 120 * time.Millisecond}, "the row is not there"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openMem(tt.store)
			defer db.Close()
			errs := failures(t, func(tb testing.TB) {
				tb.Helper()
				dbimptest.RoundTrip(tb, db, memCase())
			})
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tt.want) {
				t.Errorf("RoundTrip reported %q, want a failure that holds %q", joined, tt.want)
			}
		})
	}
}

func TestRoundTripNeedsTwoValues(t *testing.T) {
	db := openMem(&memStore{})
	defer db.Close()
	c := memCase()
	c.Values = c.Values[:1]
	errs := failures(t, func(tb testing.TB) {
		tb.Helper()
		dbimptest.RoundTrip(tb, db, c)
	})
	if len(errs) == 0 || !strings.Contains(errs[0], "needs at least 2") {
		t.Errorf("RoundTrip with one value reported %q", errs)
	}
}

// A select that returns other columns with the value names the column of
// the value.
func TestRoundTripReadsItsColumn(t *testing.T) {
	db := openMem(&memStore{})
	defer db.Close()
	c := memCase()
	c.Select, c.Column = "SELECT WIDE", 1
	dbimptest.RoundTrip(t, db, c)
}

// A store that keeps the old value on an update to NULL fails the round
// trip, unless SkipUpdate names why that update cannot run.
func TestRoundTripSkipsAnUpdate(t *testing.T) {
	db := openMem(&memStore{keepOnNull: true})
	defer db.Close()
	errs := failures(t, func(tb testing.TB) {
		tb.Helper()
		dbimptest.RoundTrip(tb, db, memCase())
	})
	if !strings.Contains(strings.Join(errs, "\n"), "after the update to null") {
		t.Errorf("an update to NULL that keeps the old value reported %q", errs)
	}
	c := memCase()
	c.SkipUpdate = func(_, to dbimptest.Value) string {
		if to.In == nil {
			return "the store keeps the old value"
		}
		return ""
	}
	dbimptest.RoundTrip(t, db, c)
}

// A database that cannot delete one row has no Delete, and the row stays.
func TestRoundTripWithoutDelete(t *testing.T) {
	db := openMem(&memStore{keep: true})
	defer db.Close()
	c := memCase()
	c.Delete = ""
	dbimptest.RoundTrip(t, db, c)
}
