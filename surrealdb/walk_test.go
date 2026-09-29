package surrealdb //nolint:testpackage // The test reads the source of the two walks, which are not exported.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"slices"
	"testing"
)

// walk names the methods that cborSets and jsonSets each hold, with the
// same text, because each walks the answer with its own decoder (D108).
var walk = []string{
	"next", "row", "readHead", "readValue", "startSet", "readSet", "readResult",
	"readFirst", "readRow", "readMember", "setFailed", "finishSet", "skipSet", "finish",
}

// TestTheWalksAreAlike holds that the walk of the answer in CBOR and in JSON
// stay the same, method for method, so that a fix in one reaches the other.
func TestTheWalksAreAlike(t *testing.T) {
	t.Parallel()
	cbor, json := bodies(t, "cborsets.go", "cborSets"), bodies(t, "jsonsets.go", "jsonSets")
	for _, name := range walk {
		c, ok := cbor[name]
		if !ok {
			t.Errorf("cborSets has no method %s", name)
			continue
		}
		if j, ok := json[name]; !ok || c != j {
			t.Errorf("the method %s of cborSets and of jsonSets differ, want the same text", name)
		}
	}
	for name := range cbor {
		if _, ok := json[name]; ok && !slices.Contains(walk, name) && cbor[name] == json[name] {
			t.Errorf("the method %s is the same in both, so it belongs in walk", name)
		}
	}
}

// bodies returns the printed body of each method of the type typ in file.
func bodies(t *testing.T, file, typ string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if id, ok2 := star.X.(*ast.Ident); !ok || !ok2 || id.Name != typ {
			continue
		}
		var b bytes.Buffer
		if err := printer.Fprint(&b, fset, fn.Body); err != nil {
			t.Fatal(err)
		}
		out[fn.Name.Name] = b.String()
	}
	return out
}
