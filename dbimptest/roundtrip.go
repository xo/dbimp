package dbimptest

import (
	"context"
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
	// Delete deletes the row with a key, which is its one argument. If it is
	// empty, RoundTrip logs that the database cannot delete one row, and
	// skips the delete and the check that the row is gone, as for InfluxDB 3
	// Core.
	Delete string
	// Column is the column of the value in the row that Select returns, for a
	// database that returns other columns with it, such as the name and the
	// time of an InfluxQL series. It is 0 by default.
	Column int
	// SkipUpdate returns why the update from one value to the next cannot
	// run, or "" when it can. RoundTrip logs the reason, and skips the update
	// and the read after it. It is for a database that cannot hold the
	// update, such as InfluxDB, which keeps the old value of a field that an
	// update leaves out as NULL. If it is nil, every update runs.
	SkipUpdate func(from, to Value) string
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
	// Named passes each argument by name, for a database that binds named
	// parameters only: the key as sql.Named("key", k), and the value as
	// sql.Named("value", v). The statements then name the key and the value,
	// such as $key and $value.
	Named bool
	// KeyArg turns the key into the argument that the statements take, such
	// as a record id. If it is nil, the argument is the key as a string.
	// Literal gets the key as a string, and writes it in the same form.
	KeyArg func(key string) any
}

// args returns the arguments of a statement: the value, if the statement
// takes one, and the key.
func (rt roundTrip) args(value any, withValue bool) []any {
	var key any = rt.key
	if rt.c.KeyArg != nil {
		key = rt.c.KeyArg(rt.key)
	}
	switch {
	case rt.c.Named && withValue:
		return []any{sql.Named("value", value), sql.Named("key", key)}
	case rt.c.Named:
		return []any{sql.Named("key", key)}
	case withValue:
		return []any{value, key}
	}
	return []any{key}
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
		// The context of the test ends before its cleanup runs.
		ctx := context.WithoutCancel(tb.Context())
		for _, stmt := range c.Teardown {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
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
	} else if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Insert, rt.insertArgs(v.In)...); err != nil {
		rt.tb.Errorf("%s: inserting: %v", label, err)
		return
	}
	if !rt.read(label+", after the insert", v.want()) {
		return
	}
	if why := rt.skipUpdate(v, next); why != "" {
		rt.tb.Logf("%s: the update to %s does not run: %s", label, next.Name, why)
	} else {
		if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Update, rt.args(next.In, true)...); err != nil {
			rt.tb.Errorf("%s: updating to %s: %v", label, next.Name, err)
			return
		}
		if !rt.read(label+", after the update to "+next.Name, next.want()) {
			return
		}
	}
	if rt.c.Delete == "" {
		rt.tb.Logf("%s: the database cannot delete one row, so the row stays", label)
		return
	}
	if _, err := rt.db.ExecContext(rt.tb.Context(), rt.c.Delete, rt.args(nil, false)...); err != nil {
		rt.tb.Errorf("%s: deleting: %v", label, err)
		return
	}
	rt.gone(label + ", after the delete")
}

// insertArgs returns the arguments of the insert: the key and the value, in
// that order, for a database that binds by position.
func (rt roundTrip) insertArgs(value any) []any {
	args := rt.args(value, true)
	if !rt.c.Named {
		args[0], args[1] = args[1], args[0]
	}
	return args
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

// skipUpdate returns why the update from v to next cannot run, or "".
func (rt roundTrip) skipUpdate(v, next Value) string {
	if rt.c.SkipUpdate == nil {
		return ""
	}
	return rt.c.SkipUpdate(v, next)
}

// selectValue selects the row of the key, and returns the value in its
// column Column. It returns sql.ErrNoRows when there is no row.
func (rt roundTrip) selectValue() (any, error) {
	rows, err := rt.db.QueryContext(rt.tb.Context(), rt.c.Select, rt.args(nil, false)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if rt.c.Column >= len(cols) {
		return nil, fmt.Errorf("the select returns %d columns, and the value is in column %d", len(cols), rt.c.Column)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals[rt.c.Column], rows.Err()
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
