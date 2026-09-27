package dbimptest

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// pollEvery is how often RoundTrip reads again while it waits for a write.
const pollEvery = 50 * time.Millisecond

// RoundTripCase is the round trip of one type, as step 14a of
// docs/DRIVER.md requires. The driver supplies the statements, because each
// database writes them in its own way. RoundTrip runs every step itself.
type RoundTripCase struct {
	// Type names the type, as the type table names it.
	Type string
	// Setup holds the statements that create the table, which RoundTrip
	// runs once, before any value.
	Setup []string
	// Teardown holds the statements that drop what Setup made. RoundTrip
	// runs them in a cleanup, so they run even when the test fails.
	Teardown []string
	// Insert inserts one row. Its arguments are the key and the value.
	Insert string
	// Literal returns a statement that inserts one row, with the key and the
	// value written as literals. It returns an error that wraps
	// dbimp.ErrNotSupported for a value that the database cannot write as a
	// literal, and RoundTrip logs that and goes on.
	Literal func(key string, v any) (string, error)
	// Select selects the value of the row with a key, which is its one
	// argument. It returns one column.
	Select string
	// Update sets the value of the row with a key. Its arguments are the
	// value and the key.
	Update string
	// Delete deletes the row with a key, which is its one argument.
	Delete string
	// Values are the values to store. RoundTrip updates each value to the
	// next one, and the last one to the first, so it needs at least two.
	Values []Value
	// Wait is how long RoundTrip reads again until a read sees the write
	// before it, for a database that updates an index after a write. Zero
	// reads once.
	Wait time.Duration
	// Equal compares a value read back with the value wanted. If it is nil,
	// RoundTrip uses reflect.DeepEqual, which also compares the Go types.
	Equal func(got, want any) bool
}

// Value is one value of a round trip.
type Value struct {
	// Name names the value, such as "max" or "unicode".
	Name string
	// In is the value to write.
	In any
	// Want is the value that a read returns into a *any. If it is nil and In
	// is not, RoundTrip wants In.
	Want any
}

func (v Value) want() any {
	if v.Want == nil {
		return v.In
	}
	return v.Want
}

// RoundTrip runs the round trip of c against db. For each value, and once
// with the value as a bound argument and once as a literal, it inserts a
// row, selects it by its key and compares the value, updates it to the next
// value, selects and compares again, deletes it, and selects to see that it
// is gone. It reads each value into a *any, so the comparison covers the Go
// type that database/sql returns, and it never selects a literal in place
// of a stored value.
func RoundTrip(tb testing.TB, db *sql.DB, c RoundTripCase) {
	tb.Helper()
	if len(c.Values) < 2 {
		tb.Fatalf("the round trip of %s has %d values, and needs at least 2", c.Type, len(c.Values))
	}
	if c.Equal == nil {
		c.Equal = reflect.DeepEqual
	}
	tb.Cleanup(func() {
		for _, stmt := range c.Teardown {
			if _, err := db.ExecContext(tb.Context(), stmt); err != nil {
				tb.Errorf("tearing down the round trip of %s: %s: %v", c.Type, stmt, err)
			}
		}
	})
	for _, stmt := range c.Setup {
		if _, err := db.ExecContext(tb.Context(), stmt); err != nil {
			tb.Fatalf("setting up the round trip of %s: %s: %v", c.Type, stmt, err)
		}
	}
	for i, v := range c.Values {
		next := c.Values[(i+1)%len(c.Values)]
		for _, literal := range []bool{false, true} {
			rt := roundTrip{tb: tb, db: db, c: c, key: key(c.Type, i, literal)}
			rt.run(v, next, literal)
		}
	}
}

// roundTrip is the round trip of one value, under one key.
type roundTrip struct {
	tb  testing.TB
	db  *sql.DB
	c   RoundTripCase
	key string
}

func (rt roundTrip) run(v, next Value, literal bool) {
	rt.tb.Helper()
	how := "as an argument"
	if literal {
		how = "as a literal"
	}
	label := fmt.Sprintf("%s %s %s", rt.c.Type, v.Name, how)
	if literal {
		stmt, err := rt.c.Literal(rt.key, v.In)
		switch {
		case errors.Is(err, dbimp.ErrNotSupported):
			rt.tb.Logf("%s: the database has no literal for it: %v", label, err)
			return
		case err != nil:
			rt.tb.Errorf("%s: writing the literal: %v", label, err)
			return
		}
		if _, err := rt.db.ExecContext(rt.tb.Context(), stmt); err != nil {
			rt.tb.Errorf("%s: inserting: %s: %v", label, stmt, err)
			return
		}
	} else if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Insert, rt.key, v.In); err != nil {
		rt.tb.Errorf("%s: inserting: %v", label, err)
		return
	}
	if !rt.read(label+", after the insert", v.want()) {
		return
	}
	if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Update, next.In, rt.key); err != nil {
		rt.tb.Errorf("%s: updating to %s: %v", label, next.Name, err)
		return
	}
	if !rt.read(label+", after the update to "+next.Name, next.want()) {
		return
	}
	if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Delete, rt.key); err != nil {
		rt.tb.Errorf("%s: deleting: %v", label, err)
		return
	}
	rt.gone(label + ", after the delete")
}

// read selects the value of the row, until it equals want or the wait ends.
func (rt roundTrip) read(label string, want any) bool {
	rt.tb.Helper()
	var (
		got any
		err error
	)
	for deadline := time.Now().Add(rt.c.Wait); ; {
		got, err = rt.selectValue()
		if err == nil && rt.c.Equal(got, want) {
			return true
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(pollEvery)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		rt.tb.Errorf("%s: the row is not there", label)
	case err != nil:
		rt.tb.Errorf("%s: selecting: %v", label, err)
	default:
		rt.tb.Errorf("%s: read %#v (%T), want %#v (%T)", label, got, got, want, want)
	}
	return false
}

// gone selects the row until it is gone or the wait ends.
func (rt roundTrip) gone(label string) {
	rt.tb.Helper()
	var err error
	for deadline := time.Now().Add(rt.c.Wait); ; {
		_, err = rt.selectValue()
		if errors.Is(err, sql.ErrNoRows) {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(pollEvery)
	}
	if err != nil {
		rt.tb.Errorf("%s: selecting: %v", label, err)
		return
	}
	rt.tb.Errorf("%s: the row is still there", label)
}

func (rt roundTrip) selectValue() (any, error) {
	var got any
	err := rt.db.QueryRowContext(rt.tb.Context(), rt.c.Select, rt.key).Scan(&got)
	return got, err
}

// key returns the key of one value of a round trip.
func key(typ string, i int, literal bool) string {
	mode := "arg"
	if literal {
		mode = "lit"
	}
	return fmt.Sprintf("dbimp-rt-%s-%d-%s", strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			return r
		}
		return '-'
	}, strings.ToLower(typ)), i, mode)
}
