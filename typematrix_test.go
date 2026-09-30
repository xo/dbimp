package dbimp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// These tests hold step 8a of docs/DRIVER.md and D137: every type of every
// driver has a kind that docs/TYPES.md names, and the table of every driver
// in docs/TYPES.md is the one that the type tables of the product documents
// make.

// typesDoc is the document of the kinds.
const typesDoc = "docs/TYPES.md"

// typeRow is one row of the type table of a product document.
type typeRow struct {
	wire, kind, goType string
}

// kindList returns the kinds of docs/TYPES.md, in their order.
func kindList(doc string) []string {
	var kinds []string
	for _, cells := range block(doc, dbimptest.KindsBegin, dbimptest.KindsEnd) {
		kinds = append(kinds, cells[0])
	}
	return kinds
}

// productTypes returns the rows of the type table of a product document.
func productTypes(doc string) []typeRow {
	var rows []typeRow
	for _, cells := range block(doc, dbimptest.TypesBegin, dbimptest.TypesEnd) {
		if len(cells) < 3 {
			continue
		}
		rows = append(rows, typeRow{wire: cells[0], kind: cells[1], goType: strings.Trim(cells[2], "`")})
	}
	return rows
}

// block returns the cells of each row of the table between begin and end,
// without the header and the line under it.
func block(doc, begin, end string) [][]string {
	i, j := strings.Index(doc, begin), strings.Index(doc, end)
	if i < 0 || j < i {
		return nil
	}
	var rows [][]string
	for n, line := range strings.Split(strings.TrimSpace(doc[i+len(begin):j]), "\n") {
		if n < 2 || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), " | ")
		for k := range cells {
			cells[k] = strings.TrimSpace(cells[k])
		}
		rows = append(rows, cells)
	}
	return rows
}

// shortGo returns the start of the text of a Go type, up to the first
// explanation, such as time.Time of "time.Time, at midnight in UTC".
func shortGo(s string) string {
	end := len(s)
	for _, sep := range []string{", ", " (", ": ", " in ", " from ", ". "} {
		if k := strings.Index(s, sep); k >= 0 && k < end {
			end = k
		}
	}
	return s[:end]
}

func TestEveryTypeHasAKind(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, k := range kindList(readFile(".", typesDoc)) {
		known[k] = true
	}
	if len(known) == 0 {
		t.Fatalf("%s has no list of kinds between %s and %s", typesDoc, dbimptest.KindsBegin, dbimptest.KindsEnd)
	}
	for _, d := range driverDirs(".") {
		for _, r := range productTypes(readFile(".", "docs", strings.ToUpper(d)+".md")) {
			if !known[r.kind] {
				t.Errorf("step 8a: the type %s of %s has the kind %q, which %s does not name", r.wire, d, r.kind, typesDoc)
			}
		}
	}
}

func TestTheTypeMatrixIsCurrent(t *testing.T) {
	t.Parallel()
	kinds := kindList(readFile(".", typesDoc))
	drivers := driverDirs(".")
	cells := map[string]map[string][]string{}
	for _, d := range drivers {
		for _, r := range productTypes(readFile(".", "docs", strings.ToUpper(d)+".md")) {
			if cells[r.kind] == nil {
				cells[r.kind] = map[string][]string{}
			}
			cells[r.kind][d] = append(cells[r.kind][d], fmt.Sprintf("%s: `%s`", r.wire, shortGo(r.goType)))
		}
	}
	var b strings.Builder
	b.WriteString("| Kind | " + strings.Join(drivers, " | ") + " |\n")
	b.WriteString("| --- |" + strings.Repeat(" --- |", len(drivers)) + "\n")
	for _, k := range kinds {
		row := []string{k}
		for _, d := range drivers {
			row = append(row, strings.Join(cells[k][d], "<br>"))
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	dbimptest.WriteBlock(t, typesDoc, dbimptest.MatrixBegin, dbimptest.MatrixEnd, b.String())
}
