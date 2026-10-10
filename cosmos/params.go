package cosmos

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// param is one entry of the list parameters of a query (recorded: "a
// parameter that is a string"). The server binds by name, and the name
// starts with @.
type param struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// parameters returns the list parameters of a query: one entry for each
// argument, in the order of the arguments. The server binds by name, and a
// positional placeholder does not exist (recorded: "parameters in another
// order"), so an argument with no name is an error (D190). A name that has no
// @ gets one, as sql.Named("p", v) names @p. The server refuses a name that
// has no @ and a name that the list gives twice, and the driver leaves both
// to it (recorded: "a parameter with no at sign", "a parameter named
// twice").
func parameters(args []driver.NamedValue) ([]param, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]param, len(args))
	for i, a := range args {
		if a.Name == "" {
			return nil, fmt.Errorf("binding the argument %d: Cosmos DB binds each parameter by its name, so use sql.Named: %w", a.Ordinal, dbimp.ErrArguments)
		}
		name := a.Name
		if !strings.HasPrefix(name, "@") {
			name = "@" + name
		}
		out[i] = param{Name: name, Value: a.Value}
	}
	return out, nil
}

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), and the values that the driver binds
// as JSON values of their own: a decimal, a list and a map. A decimal is a
// JSON number, with all its digits. A nil pointer that implements
// driver.Valuer becomes nil. It hands every other value to the default
// converter of database/sql, which gives an int64, a float64, a bool, a
// string, a []byte, which is sent as base64 text, and a time.Time, which is
// sent as RFC 3339 text, because a date is a string (D190).
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch v := nv.Value.(type) {
	case *apd.Decimal:
		return decimalValue(nv, v)
	case apd.Decimal:
		return decimalValue(nv, &v)
	case []any, map[string]any:
		return nil
	}
	if v, ok := nv.Value.(driver.Valuer); ok && isNilPointer(v) {
		nv.Value = nil
		return nil
	}
	return driver.ErrSkip
}

// decimalValue turns a decimal into a JSON number, with all its digits. A
// decimal that is not finite is not a JSON number, so it is an error.
func decimalValue(nv *driver.NamedValue, d *apd.Decimal) error {
	if d == nil {
		nv.Value = nil
		return nil
	}
	if d.Form != apd.Finite {
		return fmt.Errorf("binding the decimal %s: JSON has no such number: %w", d, dbimp.ErrInvalidValue)
	}
	nv.Value = jsontext.Value(d.String())
	return nil
}
