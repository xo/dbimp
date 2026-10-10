package bigquery_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/bigquery"
)

// TestRegistered holds D28 and D30: the driver registers the one name bigquery,
// and database/sql opens a DSN through it.
func TestRegistered(t *testing.T) {
	t.Parallel()
	if !slices.Contains(sql.Drivers(), bigquery.Name) || bigquery.Name != "bigquery" {
		t.Errorf("the drivers are %v, want bigquery", sql.Drivers())
	}
	db, err := sql.Open("bigquery", "bigquery://p/d?disable_auth=true&endpoint=http%3A%2F%2F127.0.0.1%3A1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Nothing listens on that port, so the ping fails before a request reaches a
	// server.
	if err := db.PingContext(t.Context()); err == nil {
		t.Error("a ping of a closed port gave no error")
	}
	if _, err := sql.Open("bigquery", "bigquery://p?nosuch=1"); !errors.Is(err, dbimp.ErrUnknownKey) {
		t.Errorf("a DSN with an unknown key gave %v, want dbimp.ErrUnknownKey", err)
	}
	if _, err := (bigquery.Driver{}).Open("bigquery://p"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Driver.Open gave %v, want dbimp.ErrNotSupported", err)
	}
}
