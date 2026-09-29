package dbimp_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// These tests hold the steps of docs/DRIVER.md, as W6 in docs/BACKLOG.md
// requires. Each gate reads the repository, and returns one problem for each
// step that a driver skipped. Each problem names the step, so that the
// message says what to do.

// notDrivers are the folders at the root that hold no driver.
var notDrivers = []string{"dbimptest", "docs", "testdata"}

// gate reads the repository at root, and returns its problems.
type gate func(root string) []string

// gates are every gate, by the name of the test that runs it.
var gates = map[string]gate{
	"TestEveryDriverIsATarget":              gateTargets,
	"TestEveryDriverHasItsDocument":         gateDocument,
	"TestEveryDriverHasItsManifest":         gateManifest,
	"TestEveryDriverRunsTheContract":        gateContract,
	"TestEveryDriverTestsItsDSN":            gateDSNTests,
	"TestEveryDriverGeneratesItsTables":     gateTables,
	"TestNoDriverTouchesGlobalState":        gateGlobals,
	"TestEveryDriverRegistersOneName":       gateRegister,
	"TestTheWorkflowNamesNoRelease":         gateWorkflow,
	"TestEveryDriverHasItsFeatures":         gateFeatures,
	"TestEveryDriverIsComparedWithTheFirst": gateComparison,
	"TestEveryDriverTakesTheCommonOptions":  gateOptions,
}

func runGate(t *testing.T, g gate) {
	t.Helper()
	for _, p := range g(".") {
		t.Error(p)
	}
}

func TestEveryDriverIsATarget(t *testing.T) {
	t.Parallel()
	runGate(t, gateTargets)
}

func TestEveryDriverHasItsDocument(t *testing.T) {
	t.Parallel()
	runGate(t, gateDocument)
}

func TestEveryDriverHasItsManifest(t *testing.T) {
	t.Parallel()
	runGate(t, gateManifest)
}

func TestEveryDriverRunsTheContract(t *testing.T) {
	t.Parallel()
	runGate(t, gateContract)
}

func TestEveryDriverTestsItsDSN(t *testing.T) {
	t.Parallel()
	runGate(t, gateDSNTests)
}

func TestEveryDriverGeneratesItsTables(t *testing.T) {
	t.Parallel()
	runGate(t, gateTables)
}

func TestNoDriverTouchesGlobalState(t *testing.T) {
	t.Parallel()
	runGate(t, gateGlobals)
}

func TestEveryDriverRegistersOneName(t *testing.T) {
	t.Parallel()
	runGate(t, gateRegister)
}

func TestTheWorkflowNamesNoRelease(t *testing.T) {
	t.Parallel()
	runGate(t, gateWorkflow)
}

func TestEveryDriverHasItsFeatures(t *testing.T) {
	t.Parallel()
	runGate(t, gateFeatures)
}

func TestEveryDriverIsComparedWithTheFirst(t *testing.T) {
	t.Parallel()
	runGate(t, gateComparison)
}

func TestEveryDriverTakesTheCommonOptions(t *testing.T) {
	t.Parallel()
	runGate(t, gateOptions)
}

// commonOptions are the functions of the options that every driver declares
// (D109).
var commonOptions = []string{"WithOptions", "WithTimeout", "WithReadonly", "WithParameter", "WithDatabase"}

// gateOptions holds step 12: each driver declares Option as an alias of
// dbimp.Option, and the functions of the options that every driver has
// (D109).
func gateOptions(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		funcs := map[string]bool{}
		alias := false
		for _, f := range parseDir(root, d, false) {
			pkg := importName(f, "github.com/xo/dbimp")
			for _, decl := range f.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Recv == nil {
						funcs[decl.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok || ts.Name.Name != "Option" || !ts.Assign.IsValid() {
							continue
						}
						if ix, ok := ts.Type.(*ast.IndexExpr); ok && isSelector(ix.X, pkg, "Option") {
							alias = true
						}
					}
				}
			}
		}
		if !alias {
			problems = append(problems, fmt.Sprintf("step 12: %s declares no type Option = dbimp.Option[options] (D109)", d))
		}
		for _, name := range commonOptions {
			if !funcs[name] {
				problems = append(problems, fmt.Sprintf("step 12: %s declares no %s (D109)", d, name))
			}
		}
	}
	return problems
}

// crudNames are the statements of CRUD that every survey names.
var crudNames = []string{"insert", "select", "update", "delete"}

// gateFeatures holds steps 5a, 6, 10 and 14a: each driver has a survey that
// asked two models and another driver, names each statement of CRUD, and
// settles every entry against the server, and each entry names a test that
// exists. The test of each type calls dbimptest.RoundTrip, and the type table
// of the product document has a row for each type marked yes, and no other.
func gateFeatures(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		dir := filepath.Join(root, "testdata", d)
		f, err := dbimptest.ReadFeatures(filepath.Join(dir, dbimptest.FeaturesName))
		if err != nil {
			problems = append(problems, fmt.Sprintf("step 5a: %s: %v", d, err))
			continue
		}
		counts := map[string]map[string]bool{dbimptest.SourceModel: {}, dbimptest.SourceDriver: {}}
		for _, c := range f.Consulted {
			if counts[c.Kind] != nil {
				counts[c.Kind][c.Source] = true
			}
		}
		if n := len(counts[dbimptest.SourceModel]); n < 2 {
			problems = append(problems, fmt.Sprintf("step 5a: the survey of %s asked %d models, want at least 2", d, n))
		}
		if len(counts[dbimptest.SourceDriver]) == 0 {
			problems = append(problems, fmt.Sprintf("step 5a: the survey of %s read no other driver", d))
		}
		for _, name := range crudNames {
			if !slices.ContainsFunc(f.Entries, func(e dbimptest.Feature) bool {
				return e.Kind == dbimptest.KindCRUD && e.Name == name
			}) {
				problems = append(problems, fmt.Sprintf("step 5a: the survey of %s has no entry for %s", d, name))
			}
		}
		tests := testFuncs(root, d)
		yesTypes := map[string]bool{}
		for _, e := range f.Entries {
			id := e.Kind + " " + e.Name
			switch {
			case len(e.Sources) == 0:
				problems = append(problems, fmt.Sprintf("step 5a: the entry %s of %s names no source", id, d))
			case e.Verdict == dbimptest.NotMeasured:
				problems = append(problems, fmt.Sprintf("step 6: the entry %s of %s is not measured", id, d))
			case e.Verdict != dbimptest.Yes && e.Verdict != dbimptest.No:
				problems = append(problems, fmt.Sprintf("step 6: the entry %s of %s has the verdict %q", id, d, e.Verdict))
			}
			if e.Verdict == dbimptest.No {
				if _, err := os.Stat(filepath.Join(dir, e.Evidence)); e.Evidence == "" || err != nil {
					problems = append(problems, fmt.Sprintf("step 6: the entry %s of %s is no, and names no recorded answer that shows the server lacks it", id, d))
				}
			}
			fn, _, _ := strings.Cut(e.Test, "/")
			decl, ok := tests[fn]
			switch {
			case e.Test == "":
				problems = append(problems, fmt.Sprintf("step 14a: the entry %s of %s names no test", id, d))
			case !ok:
				problems = append(problems, fmt.Sprintf("step 14a: the entry %s of %s names %s, which no test of %s defines", id, d, fn, d))
			case e.Kind == dbimptest.KindType && e.Verdict == dbimptest.Yes && !callsIn(decl, "RoundTrip"):
				problems = append(problems, fmt.Sprintf("step 14a: %s, the test of the type %s of %s, does not call dbimptest.RoundTrip", fn, e.Name, d))
			}
			if e.Kind == dbimptest.KindType && e.Verdict == dbimptest.Yes {
				yesTypes[e.Name] = true
			}
		}
		rows := typeTableRows(readFile(root, "docs", strings.ToUpper(d)+".md"))
		for _, name := range slices.Sorted(maps.Keys(yesTypes)) {
			if !rows[name] {
				problems = append(problems, fmt.Sprintf("step 10: the type table of %s has no row for %s, which the survey marks yes", d, name))
			}
		}
		for _, name := range slices.Sorted(maps.Keys(rows)) {
			if !yesTypes[name] {
				problems = append(problems, fmt.Sprintf("step 10: the type table of %s has a row for %s, which the survey does not mark yes", d, name))
			}
		}
	}
	return problems
}

// testDecl is a test function and the name under which its file imports
// dbimptest.
type testDecl struct {
	fn  *ast.FuncDecl
	pkg string
}

// testFuncs returns every function of the tests of the driver in folder d, by
// name.
func testFuncs(root, d string) map[string]testDecl {
	funcs := map[string]testDecl{}
	for _, f := range parseDir(root, d, true) {
		pkg := importName(f, "github.com/xo/dbimp/dbimptest")
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcs[fn.Name.Name] = testDecl{fn: fn, pkg: pkg}
			}
		}
	}
	return funcs
}

// callsIn reports whether the body of the function calls the function name
// of dbimptest.
func callsIn(d testDecl, name string) bool {
	found := false
	if d.fn.Body == nil {
		return false
	}
	ast.Inspect(d.fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isSelector(call.Fun, d.pkg, name) {
			found = true
		}
		return !found
	})
	return found
}

// typeTableRows returns the wire type of each row of the type table between
// its markers in a product document.
func typeTableRows(doc string) map[string]bool {
	rows := map[string]bool{}
	i, j := strings.Index(doc, dbimptest.TypesBegin), strings.Index(doc, dbimptest.TypesEnd)
	if i < 0 || j < i {
		return rows
	}
	for line := range strings.SplitSeq(doc[i+len(dbimptest.TypesBegin):j], "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		name := strings.TrimSpace(cells[1])
		if name == "" || name == "Wire type" || strings.HasPrefix(name, "---") {
			continue
		}
		rows[name] = true
	}
	return rows
}

// gateTargets holds step 17: the row of a driver in docs/TARGETS.md names its
// package, and every package that docs/TARGETS.md names exists.
func gateTargets(root string) []string {
	targets := readFile(root, "docs", "TARGETS.md")
	var problems []string
	dirs := driverDirs(root)
	for _, d := range dirs {
		if !strings.Contains(targets, "`github.com/xo/dbimp/"+d+"`") {
			problems = append(problems, fmt.Sprintf("step 17: the row of %s in docs/TARGETS.md does not name `github.com/xo/dbimp/%s`", d, d))
		}
	}
	for _, m := range regexp.MustCompile("`github\\.com/xo/dbimp/([a-z0-9]+)`").FindAllStringSubmatch(targets, -1) {
		if !slices.Contains(dirs, m[1]) {
			problems = append(problems, fmt.Sprintf("step 17: docs/TARGETS.md names github.com/xo/dbimp/%s, which has no folder", m[1]))
		}
	}
	return problems
}

// gateDocument holds step 8: each driver has docs/<PRODUCT>.md with
// every heading of the template.
func gateDocument(root string) []string {
	headings := templateHeadings(root)
	var problems []string
	for _, d := range driverDirs(root) {
		name := filepath.Join("docs", strings.ToUpper(d)+".md")
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			problems = append(problems, fmt.Sprintf("step 8: %s has no %s", d, name))
			continue
		}
		for _, h := range headings {
			if !regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(h) + `$`).Match(body) {
				problems = append(problems, fmt.Sprintf("step 8: %s has no heading %q from the template", name, "## "+h))
			}
		}
	}
	return problems
}

// firstDriver is the first driver, which every other driver is compared
// with in step 17a (D97).
const firstDriver = "couchbase"

// The headings of the comparison of step 17a (D97).
const (
	comparedHeading = "## Compared with Couchbase"
	serverHeading   = "### The server"
	driverHeading   = "### The driver"
)

// gateComparison holds step 17a: the document of each driver other than the
// first holds its comparison with the first, in two parts.
func gateComparison(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		if d == firstDriver {
			continue
		}
		name := filepath.Join("docs", strings.ToUpper(d)+".md")
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			problems = append(problems, fmt.Sprintf("step 17a: %s has no %s to hold its comparison with %s", d, name, firstDriver))
			continue
		}
		compared := section(string(body), comparedHeading)
		if compared == "" {
			problems = append(problems, fmt.Sprintf("step 17a: %s has no heading %q", name, comparedHeading))
			continue
		}
		for _, h := range []string{serverHeading, driverHeading} {
			if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(h) + `$`).MatchString(compared) {
				problems = append(problems, fmt.Sprintf("step 17a: %s has no heading %q under %q", name, h, comparedHeading))
			}
		}
	}
	return problems
}

// gateManifest holds step 6: the manifest of each driver names a file, or a
// reason, for each item and each principal, and every file under the
// testdata of the driver is in the manifest.
func gateManifest(root string) []string {
	items := step6Items(root)
	var problems []string
	for _, d := range driverDirs(root) {
		dir := filepath.Join(root, "testdata", d)
		m, err := dbimptest.ReadManifest(filepath.Join(dir, dbimptest.ManifestName))
		if err != nil {
			problems = append(problems, fmt.Sprintf("step 6: %s: %v", d, err))
			continue
		}
		principals := []string{dbimptest.Administrator, dbimptest.Ordinary}
		if m.NoOrdinaryUser != "" {
			principals = principals[:1]
		}
		named := map[string]bool{dbimptest.ManifestName: true, dbimptest.RequestsName: true, dbimptest.FeaturesName: true}
		for item := 1; item <= items; item++ {
			for _, p := range principals {
				i := slices.IndexFunc(m.Entries, func(e dbimptest.Entry) bool {
					return e.Item == item && e.Principal == p
				})
				if i < 0 {
					problems = append(problems, fmt.Sprintf("step 6: the manifest of %s has no entry for item %d as the %s user", d, item, p))
				}
			}
		}
		for _, e := range m.Entries {
			switch {
			case e.File == "" && e.Absent == "":
				problems = append(problems, fmt.Sprintf("step 6: the entry for item %d of %s names neither a file nor a reason", e.Item, d))
			case e.File != "":
				named[e.File] = true
				if _, err := os.Stat(filepath.Join(dir, e.File)); err != nil {
					problems = append(problems, fmt.Sprintf("step 6: the manifest of %s names %s, which does not exist", d, e.File))
				}
			}
		}
		_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && !named[filepath.Base(path)] {
				problems = append(problems, fmt.Sprintf("step 6: %s is not in the manifest of %s", path, d))
			}
			return nil
		})
	}
	return problems
}

// gateContract holds step 11: the tests of each driver call RunContract.
func gateContract(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		if !callsHelper(parseDir(root, d, true), "RunContract") {
			problems = append(problems, fmt.Sprintf("step 11: the tests of %s do not call dbimptest.RunContract", d))
		}
	}
	return problems
}

// gateDSNTests holds step 13: each driver has a fuzz test and a round trip
// test for its DSN.
func gateDSNTests(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		var fuzz, roundTrip bool
		for _, f := range parseDir(root, d, true) {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				name := fn.Name.Name
				fuzz = fuzz || strings.HasPrefix(name, "Fuzz")
				roundTrip = roundTrip || strings.HasPrefix(name, "Test") && strings.Contains(name, "RoundTrip")
			}
		}
		if !fuzz {
			problems = append(problems, fmt.Sprintf("step 13: %s has no fuzz test for its DSN", d))
		}
		if !roundTrip {
			problems = append(problems, fmt.Sprintf("step 13: %s has no test whose name holds RoundTrip for its DSN", d))
		}
	}
	return problems
}

// gateTables holds step 10: the document of each driver holds the markers
// of the two tables, and its tests make both tables.
func gateTables(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		doc := readFile(root, "docs", strings.ToUpper(d)+".md")
		for _, marker := range []string{dbimptest.TypesBegin, dbimptest.TypesEnd, dbimptest.InterfacesBegin, dbimptest.InterfacesEnd} {
			if !strings.Contains(doc, marker) {
				problems = append(problems, fmt.Sprintf("step 10: docs/%s.md has no %s", strings.ToUpper(d), marker))
			}
		}
		files := parseDir(root, d, true)
		for _, helper := range []string{"TypeTable", "InterfaceTable"} {
			if !callsHelper(files, helper) {
				problems = append(problems, fmt.Sprintf("step 10: the tests of %s do not call dbimptest.%s", d, helper))
			}
		}
	}
	return problems
}

// gateGlobals holds step 12 and D7: no driver changes a default of net/http,
// reads a whole body with io.ReadAll, or assigns to a package variable
// outside init. The last check is a heuristic: it counts an assignment to a
// name that the function does not declare itself.
func gateGlobals(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		files := parseDir(root, d, false)
		globals := map[string]bool{}
		for _, f := range files {
			for _, decl := range f.Decls {
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
					for _, spec := range gd.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							for _, n := range vs.Names {
								if n.Name != "_" {
									globals[n.Name] = true
								}
							}
						}
					}
				}
			}
		}
		for _, f := range files {
			http, io := importName(f, "net/http"), importName(f, "io")
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				locals := declaredNames(fn)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.CallExpr:
						if isSelector(n.Fun, io, "ReadAll") {
							problems = append(problems, fmt.Sprintf("step 12: %s calls io.ReadAll in %s, and must read the body one token at a time (D25 and D36)", d, fn.Name.Name))
						}
					case *ast.AssignStmt:
						for _, lhs := range n.Lhs {
							if isSelector(lhs, http, "DefaultTransport") || isSelector(lhs, http, "DefaultClient") {
								problems = append(problems, fmt.Sprintf("step 12: %s assigns to a default of net/http in %s (D7)", d, fn.Name.Name))
							}
							if id, ok := lhs.(*ast.Ident); ok && globals[id.Name] && !locals[id.Name] && fn.Name.Name != "init" {
								problems = append(problems, fmt.Sprintf("step 12: %s assigns to the package variable %s in %s (D7)", d, id.Name, fn.Name.Name))
							}
						}
					}
					return true
				})
			}
		}
	}
	return problems
}

// gateRegister holds step 12, D28 and D30: each driver calls sql.Register
// once, from init, with the name of its folder.
func gateRegister(root string) []string {
	var problems []string
	for _, d := range driverDirs(root) {
		var names []string
		var outside bool
		files := parseDir(root, d, false)
		consts := stringConsts(files)
		for _, f := range files {
			sqlName := importName(f, "database/sql")
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || !isSelector(call.Fun, sqlName, "Register") {
						return true
					}
					outside = outside || fn.Name.Name != "init"
					name := "?"
					if len(call.Args) > 0 {
						switch arg := call.Args[0].(type) {
						case *ast.BasicLit:
							if arg.Kind == token.STRING {
								name, _ = strconv.Unquote(arg.Value)
							}
						case *ast.Ident:
							if v, ok := consts[arg.Name]; ok {
								name = v
							}
						}
					}
					names = append(names, name)
					return true
				})
			}
		}
		switch {
		case len(names) != 1:
			problems = append(problems, fmt.Sprintf("step 12: %s calls sql.Register %d times, want once (D28)", d, len(names)))
		case names[0] != d:
			problems = append(problems, fmt.Sprintf("step 12: %s registers %q, want the name of its folder, %q (D30)", d, names[0], d))
		}
		if outside {
			problems = append(problems, fmt.Sprintf("step 12: %s calls sql.Register outside init", d))
		}
	}
	return problems
}

// stringConsts returns the string constants of a package, by name, such as
// the Name that a driver registers.
func stringConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, n := range vs.Names {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						consts[n.Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
		}
	}
	return consts
}

// releaseName matches a release as dbrun names it, such as couchbase-8.0.3.
var releaseName = regexp.MustCompile(`\b[a-z][a-z0-9]*-[0-9]+(\.[0-9]+)*\b`)

// gateWorkflow holds step 15: a workflow names no release, and a workflow
// that runs the integration tests reads the releases from dbrun.
func gateWorkflow(root string) []string {
	paths, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	var problems []string
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, m := range releaseName.FindAllString(string(body), -1) {
			problems = append(problems, fmt.Sprintf("step 15: %s names the release %s, and must read it from dbrun list (dbmeta D69)", path, m))
		}
		if strings.Contains(string(body), "-run Integration") && !strings.Contains(string(body), "dbrun list --json") {
			problems = append(problems, fmt.Sprintf("step 15: %s runs the integration tests and does not read the releases from dbrun list --json", path))
		}
	}
	return problems
}

// driverDirs returns the folder of every driver: each folder at the root
// that holds Go code, except the ones in notDrivers and the hidden ones.
func driverDirs(root string) []string {
	entries, _ := os.ReadDir(root)
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || slices.Contains(notDrivers, name) {
			continue
		}
		if matches, _ := filepath.Glob(filepath.Join(root, name, "*.go")); len(matches) > 0 {
			dirs = append(dirs, name)
		}
	}
	return dirs
}

// templateHeadings returns the headings of the template in docs/DRIVER.md.
func templateHeadings(root string) []string {
	return numberedItems(section(readFile(root, "docs", "DRIVER.md"), "## The template for a product document"), true)
}

// step6Items returns the number of items that step 6 of docs/DRIVER.md
// measures.
func step6Items(root string) int {
	return len(numberedItems(section(readFile(root, "docs", "DRIVER.md"), "### 6. "), false))
}

// section returns the text from the heading that starts with heading, and
// the heading itself, up to the next heading of the same level or higher.
func section(doc, heading string) string {
	i := strings.Index(doc, "\n"+heading)
	if i < 0 {
		return ""
	}
	rest := doc[i+1:]
	level := strings.Index(heading, " ")
	next := regexp.MustCompile(`(?m)^#{1,`+strconv.Itoa(level)+`} `).FindAllStringIndex(rest, 2)
	if len(next) < 2 {
		return rest
	}
	return rest[:next[1][0]]
}

// numberedItems returns each item of the numbered lists in text. If names is
// true, it returns the text of each item before its first colon.
func numberedItems(text string, names bool) []string {
	var items []string
	for _, m := range regexp.MustCompile(`(?m)^(\d+)\. (.*)$`).FindAllStringSubmatch(text, -1) {
		item := m[2]
		if names {
			item, _, _ = strings.Cut(item, ":")
		}
		items = append(items, item)
	}
	return items
}

// parseDir parses the Go files of the driver in folder d: its tests if tests
// is true, and its code otherwise.
func parseDir(root, d string, tests bool) []*ast.File {
	paths, _ := filepath.Glob(filepath.Join(root, d, "*.go"))
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") != tests {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		files = append(files, f)
	}
	return files
}

// callsHelper reports whether a file calls the function name of dbimptest.
func callsHelper(files []*ast.File, name string) bool {
	for _, f := range files {
		pkg := importName(f, "github.com/xo/dbimp/dbimptest")
		found := false
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && isSelector(call.Fun, pkg, name) {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// importName returns the name under which f imports path, or "" if it does
// not import it.
func importName(f *ast.File, path string) string {
	for _, spec := range f.Imports {
		if p, _ := strconv.Unquote(spec.Path.Value); p == path {
			if spec.Name != nil {
				return spec.Name.Name
			}
			return path[strings.LastIndex(path, "/")+1:]
		}
	}
	return ""
}

// isSelector reports whether e is pkg.name.
func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || pkg == "" || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// declaredNames returns every name that fn declares: its receiver, its
// parameters, its results, and each name that its body declares.
func declaredNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for _, list := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if list == nil {
			continue
		}
		for _, field := range list.List {
			for _, n := range field.Names {
				names[n.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, lhs := range n.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				names[id.Name] = true
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if id, ok := e.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		case *ast.FuncLit:
			for _, field := range n.Type.Params.List {
				for _, id := range field.Names {
					names[id.Name] = true
				}
			}
		}
		return true
	})
	return names
}

// readFile returns the text of the file at the path under root, or "" if it
// does not exist.
func readFile(root string, path ...string) string {
	b, _ := os.ReadFile(filepath.Join(append([]string{root}, path...)...))
	return string(b)
}
