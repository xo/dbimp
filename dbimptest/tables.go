package dbimptest

import (
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// EnvUpdate is the environment variable that makes TypeTable and
// InterfaceTable write their table into the document, rather than compare
// it.
const EnvUpdate = "DBIMP_UPDATE"

// The markers around a generated table in docs/<PRODUCT>.md.
const (
	TypesBegin      = "<!-- dbimp:types -->"
	TypesEnd        = "<!-- /dbimp:types -->"
	InterfacesBegin = "<!-- dbimp:interfaces -->"
	InterfacesEnd   = "<!-- /dbimp:interfaces -->"
)

// The markers around the list of kinds, and around the generated table of
// every driver, in docs/TYPES.md (step 8a).
const (
	KindsBegin  = "<!-- dbimp:kinds -->"
	KindsEnd    = "<!-- /dbimp:kinds -->"
	MatrixBegin = "<!-- dbimp:matrix -->"
	MatrixEnd   = "<!-- /dbimp:matrix -->"
)

// TypeRow is one row of the type table of step 10 of docs/DRIVER.md.
type TypeRow struct {
	// Wire is the name of the type on the wire, such as "number".
	Wire string
	// Kind is the kind of the type, one of the kinds of docs/TYPES.md, such
	// as "integer" (step 8a of docs/DRIVER.md). Kinds sets it.
	Kind string
	// Go is the Go type of a value, such as "int64".
	Go string
	// ScanType is the type that ColumnTypeScanType returns.
	ScanType string
	// DatabaseType is the name that ColumnTypeDatabaseTypeName returns, in
	// upper case.
	DatabaseType string
	// Nullable is true if a column of the type can be NULL.
	Nullable bool
}

// TypeTable compares rows, as the type table, with the table between
// TypesBegin and TypesEnd in the document at doc, and fails the test if the
// document holds another table. If EnvUpdate is set, it writes the table into
// the document.
func TypeTable(t *testing.T, doc string, rows []TypeRow) {
	t.Helper()
	var b strings.Builder
	b.WriteString("| Wire type | Kind | Go type | Scan type | Database type | Can be NULL |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, r := range rows {
		if r.DatabaseType != strings.ToUpper(r.DatabaseType) {
			t.Errorf("the database type %q of %q is not in upper case (step 10)", r.DatabaseType, r.Wire)
		}
		if r.Kind == "" {
			t.Errorf("the type %q has no kind (step 8a)", r.Wire)
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | `%s` | `%s` | %s |\n", r.Wire, r.Kind, r.Go, r.ScanType, r.DatabaseType, yesNo(r.Nullable))
	}
	writeBlock(t, doc, TypesBegin, TypesEnd, b.String())
}

// Kinds returns rows with the kind of each row set from kinds, by the name
// of its wire type (step 8a). It fails the test for a row that kinds does
// not name, and for a name of kinds that no row has.
func Kinds(t *testing.T, rows []TypeRow, kinds map[string]string) []TypeRow {
	t.Helper()
	out := make([]TypeRow, len(rows))
	used := map[string]bool{}
	for i, r := range rows {
		kind, ok := kinds[r.Wire]
		if !ok {
			t.Errorf("the type %q has no kind (step 8a)", r.Wire)
		}
		r.Kind, used[r.Wire] = kind, true
		out[i] = r
	}
	for wire := range kinds {
		if !used[wire] {
			t.Errorf("the kinds name the type %q, which the table does not have", wire)
		}
	}
	return out
}

// interfaces are the optional interfaces of database/sql/driver that the
// interface table covers, in the order of step 10 of docs/DRIVER.md.
var interfaces = []struct {
	name string
	typ  reflect.Type
}{
	{"driver.DriverContext", reflect.TypeFor[driver.DriverContext]()},
	{"driver.Connector", reflect.TypeFor[driver.Connector]()},
	{"io.Closer on the connector", reflect.TypeFor[io.Closer]()},
	{"driver.Pinger", reflect.TypeFor[driver.Pinger]()},
	{"driver.SessionResetter", reflect.TypeFor[driver.SessionResetter]()},
	{"driver.Validator", reflect.TypeFor[driver.Validator]()},
	{"driver.NamedValueChecker", reflect.TypeFor[driver.NamedValueChecker]()},
	{"driver.QueryerContext", reflect.TypeFor[driver.QueryerContext]()},
	{"driver.ExecerContext", reflect.TypeFor[driver.ExecerContext]()},
	{"driver.ConnPrepareContext", reflect.TypeFor[driver.ConnPrepareContext]()},
	{"driver.ConnBeginTx", reflect.TypeFor[driver.ConnBeginTx]()},
	{"driver.RowsColumnScanner", reflect.TypeFor[driver.RowsColumnScanner]()},
	{"driver.RowsNextResultSet", reflect.TypeFor[driver.RowsNextResultSet]()},
	{"driver.RowsColumnTypeScanType", reflect.TypeFor[driver.RowsColumnTypeScanType]()},
	{"driver.RowsColumnTypeDatabaseTypeName", reflect.TypeFor[driver.RowsColumnTypeDatabaseTypeName]()},
	{"driver.RowsColumnTypeLength", reflect.TypeFor[driver.RowsColumnTypeLength]()},
	{"driver.RowsColumnTypeNullable", reflect.TypeFor[driver.RowsColumnTypeNullable]()},
	{"driver.RowsColumnTypePrecisionScale", reflect.TypeFor[driver.RowsColumnTypePrecisionScale]()},
}

// InterfaceTable compares the interface table of step 10 of docs/DRIVER.md
// with the table between InterfacesBegin and InterfacesEnd in the document at
// doc, and writes it there when EnvUpdate is set. values are the types of
// the driver, such as its driver, its connector, a connection and its rows.
// An interface is implemented if one of values implements it. reasons holds,
// for each interface, why the driver implements it or does not, and the test
// fails if one is missing. The connector is the only value that must
// implement io.Closer.
func InterfaceTable(t *testing.T, doc string, reasons map[string]string, connector driver.Connector, values ...any) {
	t.Helper()
	var b strings.Builder
	b.WriteString("| Interface | Implemented | Reason |\n")
	b.WriteString("| --- | --- | --- |\n")
	values = append(values, connector)
	for _, iface := range interfaces {
		reason, ok := reasons[iface.name]
		if !ok {
			t.Errorf("the interface table has no reason for %s (step 10)", iface.name)
		}
		implemented := false
		if iface.name == "io.Closer on the connector" {
			_, implemented = connector.(io.Closer)
		} else {
			for _, v := range values {
				if reflect.TypeOf(v).Implements(iface.typ) {
					implemented = true
				}
			}
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", iface.name, yesNo(implemented), reason)
	}
	for name := range reasons {
		if !knownInterface(name) {
			t.Errorf("the interface table has a reason for %s, which it does not cover", name)
		}
	}
	writeBlock(t, doc, InterfacesBegin, InterfacesEnd, b.String())
}

func knownInterface(name string) bool {
	for _, iface := range interfaces {
		if iface.name == name {
			return true
		}
	}
	return false
}

// WriteBlock compares the text between begin and end in the document at doc
// with want, or writes want there if EnvUpdate is set, as TypeTable does for
// its table. The table of docs/TYPES.md is one such block.
func WriteBlock(t *testing.T, doc, begin, end, want string) {
	t.Helper()
	writeBlock(t, doc, begin, end, want)
}

// writeBlock compares the text between begin and end in the document at doc
// with want, or writes want there if EnvUpdate is set.
func writeBlock(t *testing.T, doc, begin, end, want string) {
	t.Helper()
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("reading %s: %v", doc, err)
	}
	text := string(b)
	i, j := strings.Index(text, begin), strings.Index(text, end)
	if i < 0 || j < i {
		t.Fatalf("%s has no %s and %s around its table (step 10)", doc, begin, end)
	}
	have := strings.TrimPrefix(text[i+len(begin):j], "\n")
	if have == want {
		return
	}
	if os.Getenv(EnvUpdate) == "" {
		t.Errorf("the table in %s is not the one that the code makes. Run the test with %s=1 to write it.\nhave:\n%s\nwant:\n%s", doc, EnvUpdate, have, want)
		return
	}
	text = text[:i+len(begin)] + "\n" + want + text[j:]
	if err := os.WriteFile(doc, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", doc, err)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
