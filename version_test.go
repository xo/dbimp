package dbimp_test

import (
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/xo/dbimp"
)

func TestIsVersionQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query string
		want  bool
	}{
		{"SELECT version()", true},
		{"select version()", true},
		{"SeLeCt VeRsIoN()", true},
		{"select version();", true},
		{"  \n\tselect version() ;  \n", true},
		{"select\n\tversion()", true},
		{"SELECT  version()", true},
		{"", false},
		{";", false},
		{"select", false},
		{"selectversion()", false},
		{"select version", false},
		{"select version( )", false},
		{"select version ()", false},
		{"select version();;", false},
		{"select version() from t", false},
		{"select version(), 1", false},
		{"select 'version()'", false},
		{"select version(1)", false},
		{"select @@version", false},
		{"/* c */ select version()", false},
		{"-- c\nselect version()", false},
		{"select version() -- c", false},
		{"select /* c */ version()", false},
		{"select version(); select 1", false},
		{"select 1; select version()", false},
		{"select version(); select version()", false},
		{"explain select version()", false},
		{"select\u00a0version()", false},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			if got := dbimp.IsVersionQuery(tt.query); got != tt.want {
				t.Errorf("dbimp.IsVersionQuery(%q) = %t, want %t", tt.query, got, tt.want)
			}
		})
	}
}

func TestVersionRows(t *testing.T) {
	t.Parallel()
	r := dbimp.NewVersionRows("9.5.3")
	if cols := r.Columns(); len(cols) != 1 || cols[0] != "version" {
		t.Fatalf("Columns() = %v, want [version]", cols)
	}
	dest := make([]driver.Value, 1)
	if err := r.Next(dest); err != nil {
		t.Fatalf("Next() = %v", err)
	}
	if dest[0] != "9.5.3" {
		t.Errorf("the value is %#v, want %q", dest[0], "9.5.3")
	}
	if err := r.Next(dest); !errors.Is(err, io.EOF) {
		t.Errorf("the second Next() = %v, want io.EOF", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
}
