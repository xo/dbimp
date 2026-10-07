package dbimp

import (
	"database/sql/driver"
	"io"
	"strings"
)

// IsVersionQuery reports whether query is the statement SELECT version(),
// which a driver answers itself when its product has no query for the
// release (D181). It ignores case, the white space around the statement, and
// one final semicolon. White space must separate SELECT from version(), and
// none can stand inside the parentheses. It accepts no argument, no comment,
// and no other text, so any other statement is for the product.
func IsVersionQuery(query string) bool {
	q := strings.TrimSpace(query)
	q = strings.TrimSpace(strings.TrimSuffix(q, ";"))
	const word = "select"
	if len(q) <= len(word) || !strings.EqualFold(q[:len(word)], word) {
		return false
	}
	rest := q[len(word):]
	body := strings.TrimLeft(rest, " \t\r\n\v\f")
	if body == rest {
		return false
	}
	return strings.EqualFold(body, "version()")
}

// NewVersionRows returns the result of SELECT version(): one row and one
// column, named version, which holds release as the product writes it (D181).
func NewVersionRows(release string) driver.Rows {
	return &versionRows{release: release}
}

// versionRows is the result that NewVersionRows returns.
type versionRows struct {
	release string
	done    bool
}

// Columns satisfies driver.Rows.
func (r *versionRows) Columns() []string {
	return []string{"version"}
}

// Close satisfies driver.Rows.
func (r *versionRows) Close() error {
	return nil
}

// Next satisfies driver.Rows.
func (r *versionRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.release
	return nil
}
