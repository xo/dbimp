package dbimp_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// These tests hold the gates themselves. A complete driver in a repository
// of its own must pass every gate, and an incomplete one must fail every
// gate, each with a message that names a step.

// writeTree writes files, by path, under a new folder, with the real
// docs/DRIVER.md, and returns the folder.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["docs/DRIVER.md"] = readFile(".", "docs", "DRIVER.md")
	for path, body := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// completeDriver returns the files of a driver named good that takes every
// step that the gates hold.
func completeDriver(t *testing.T) map[string]string {
	t.Helper()
	var doc strings.Builder
	doc.WriteString("# Good\n\n")
	for _, h := range templateHeadings(".") {
		fmt.Fprintf(&doc, "## %s\n\nText.\n\n", h)
	}
	doc.WriteString(dbimptest.TypesBegin + "\n| Wire type | Go type | Scan type | Database type | Can be NULL |\n| --- | --- | --- | --- | --- |\n| number | `int64` | `int64` | `NUMBER` | yes |\n" + dbimptest.TypesEnd + "\n")
	doc.WriteString(dbimptest.InterfacesBegin + "\n" + dbimptest.InterfacesEnd + "\n")

	files := map[string]string{
		"docs/TARGETS.md": "| Good | The driver is `github.com/xo/dbimp/good`. |\n",
		"docs/GOOD.md":    doc.String(),
		"good/good.go": `package good

import (
	"database/sql"
	"net/http"
)

var client = http.DefaultClient

func init() {
	sql.Register("good", nil)
}

func use() {
	client := &http.Client{}
	_ = client
	for i, v := range []int{1} {
		_, _ = i, v
	}
}
`,
		"good/good_test.go": `package good_test

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

func TestContract(t *testing.T) { dbimptest.RunContract(t, dbimptest.Contract{}) }
func TestTables(t *testing.T) {
	dbimptest.TypeTable(t, "", nil)
	dbimptest.InterfaceTable(t, "", nil, nil)
}
func TestDSNRoundTrip(t *testing.T) {}
func FuzzParseDSN(f *testing.F)    {}
func TestCRUD(t *testing.T)        {}
func TestRefusals(t *testing.T)    {}
func TestTypes(t *testing.T) {
	dbimptest.RoundTrip(t, nil, dbimptest.RoundTripCase{})
}
`,
		".github/workflows/test.yml": "run: go run ./cmd/dbrun list --json --names tested\nrun: go test -run Integration ./...\n",
	}
	m := &dbimptest.Manifest{Driver: "good"}
	for item := 1; item <= step6Items("."); item++ {
		for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
			name := fmt.Sprintf("%03d-%s.json", item, p)
			files["testdata/good/"+name] = "{}"
			m.Entries = append(m.Entries, dbimptest.Entry{Item: item, Principal: p, Release: "good-1.0", Date: "2026-09-27", File: name})
		}
	}
	root := t.TempDir()
	if err := dbimptest.WriteManifest(filepath.Join(root, "manifest.json"), m); err != nil {
		t.Fatal(err)
	}
	files["testdata/good/manifest.json"] = readFile(root, "manifest.json")
	features := `{"driver": "good",
  "consulted": [
    {"source": "model-a", "kind": "model", "date": "2026-09-27"},
    {"source": "model-b", "kind": "model", "date": "2026-09-27"},
    {"source": "example.com/other", "kind": "driver", "date": "2026-09-27"}
  ],
  "entries": [
    {"kind": "crud", "name": "insert", "sources": ["model-a"], "verdict": "yes", "test": "TestCRUD/insert"},
    {"kind": "crud", "name": "select", "sources": ["model-a"], "verdict": "yes", "test": "TestCRUD/select"},
    {"kind": "crud", "name": "update", "sources": ["model-b"], "verdict": "yes", "test": "TestCRUD/update"},
    {"kind": "crud", "name": "delete", "sources": ["model-b"], "verdict": "yes", "test": "TestCRUD/delete"},
    {"kind": "schema", "name": "foreign key", "sources": ["model-a"], "verdict": "no", "evidence": "001-administrator.json", "test": "TestRefusals/foreign-key"},
    {"kind": "type", "name": "number", "sources": ["model-a", "model-b"], "verdict": "yes", "test": "TestTypes/number"}
  ]
}`
	files["testdata/good/features.json"] = features
	return files
}

func TestTheGatesPassACompleteDriver(t *testing.T) {
	t.Parallel()
	if n := step6Items("."); n == 0 {
		t.Fatal("found no items in step 6 of docs/DRIVER.md")
	}
	if n := len(templateHeadings(".")); n == 0 {
		t.Fatal("found no headings in the template of docs/DRIVER.md")
	}
	root := writeTree(t, completeDriver(t))
	if dirs := driverDirs(root); !slices.Equal(dirs, []string{"good"}) {
		t.Fatalf("the drivers are %q, want [good]", dirs)
	}
	for _, name := range slices.Sorted(maps.Keys(gates)) {
		for _, p := range gates[name](root) {
			t.Errorf("%s: %s", name, p)
		}
	}
}

func TestTheGatesCatchAnIncompleteDriver(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"docs/TARGETS.md": "The driver is `github.com/xo/dbimp/gone`.\n",
		"bad/bad.go": `package bad

import (
	"database/sql"
	"io"
	"net/http"
)

var state int

func init() {
	sql.Register("other", nil)
}

func read(r io.Reader) {
	_, _ = io.ReadAll(r)
	http.DefaultTransport = nil
	state = 1
	sql.Register("bad", nil)
}
`,
		"testdata/bad/stray.json": "{}",
		"testdata/bad/manifest.json": `{"driver": "bad", "entries": [
  {"item": 1, "principal": "administrator", "release": "bad-1.0", "date": "2026-09-27", "file": "missing.json"},
  {"item": 2, "principal": "administrator", "release": "bad-1.0", "date": "2026-09-27"}
]}`,
		".github/workflows/test.yml": "run: dbrun start couchbase-8.0.3\nrun: go test -run Integration ./...\n",
	})
	for name, g := range gates {
		problems := g(root)
		if len(problems) == 0 {
			t.Errorf("%s found nothing wrong with an incomplete driver", name)
		}
		for _, p := range problems {
			if !strings.HasPrefix(p, "step ") {
				t.Errorf("%s: the problem %q does not name a step", name, p)
			}
		}
	}
}
