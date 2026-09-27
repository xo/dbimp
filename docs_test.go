package dbimp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// These tests read the repository rather than the package. They hold the
// layout of D3: three documents at the root, every other one in docs/, and
// every one of those named in the tables in CLAUDE.md and README.md.

// rootDocs are the only Markdown files at the root.
var rootDocs = []string{"CLAUDE.md", "CONTRIBUTING.md", "README.md"}

// markdownLink matches a relative link to a Markdown file.
var markdownLink = regexp.MustCompile(`\]\((?:\./)?([^)#:]+\.md)(#[^)]*)?\)`)

// bareMention matches a document named in running text, which is how a Go
// comment or a workflow comment points at one, as in docs/PLAN.md.
var bareMention = regexp.MustCompile(`(?:^|[\s` + "`" + `(])((?:docs/)?[A-Z][A-Z_]*\.md)`)

// decisionRef matches a reference to a decision, such as D17.
var decisionRef = regexp.MustCompile(`\bD([1-9][0-9]?)\b`)

// otherRepo matches the name of a sibling repository before a decision
// number, as in "dbmeta D62". Such a reference names a decision of that
// repository, not of this one.
var otherRepo = regexp.MustCompile("(?:cql|dbmeta|dburl|n1ql|usql)[^\\s]*`?[ (]*$")

func TestTheRootHoldsThreeDocuments(t *testing.T) {
	t.Parallel()
	matches, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(matches, rootDocs) {
		t.Errorf("the root holds %v, want %v. Every other document goes in docs/ (D3)", matches, rootDocs)
	}
}

func TestEveryDocumentIsInTheTable(t *testing.T) {
	t.Parallel()
	docs, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("docs/ holds no document")
	}
	for _, table := range []string{"CLAUDE.md", "README.md"} {
		body := read(t, table)
		for _, doc := range docs {
			if !strings.Contains(body, filepath.ToSlash(doc)) {
				t.Errorf("%s does not name %s, so no reader finds it (D3)", table, doc)
			}
		}
	}
}

func TestEveryLinkResolves(t *testing.T) {
	t.Parallel()
	for _, path := range repoFiles(t, ".md", ".go", ".yml") {
		body := read(t, path)
		for _, m := range markdownLink.FindAllStringSubmatch(body, -1) {
			target := filepath.Join(filepath.Dir(path), m[1])
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s: the link to %s does not resolve", path, m[1])
			}
		}
		if strings.HasSuffix(path, ".md") {
			continue
		}
		for _, m := range bareMention.FindAllStringSubmatch(body, -1) {
			if _, err := os.Stat(m[1]); err != nil {
				t.Errorf("%s: names %s, which is not a file", path, m[1])
			}
		}
	}
}

func TestEveryDecisionReferenceExists(t *testing.T) {
	t.Parallel()
	written := decisions(t)
	for _, path := range repoFiles(t, ".md", ".go", ".yml") {
		body := read(t, path)
		for _, m := range decisionRef.FindAllStringSubmatchIndex(body, -1) {
			if otherRepo.MatchString(body[max(0, m[0]-24):m[0]]) {
				continue
			}
			if n := body[m[2]:m[3]]; !written[n] {
				t.Errorf("%s: refers to D%s, which is not in docs/PLAN.md", path, n)
			}
		}
	}
}

func TestTheDecisionIndexIsComplete(t *testing.T) {
	t.Parallel()
	plan := read(t, filepath.Join("docs", "PLAN.md"))
	indexed := make(map[string]bool)
	for _, m := range regexp.MustCompile(`(?m)^\| \[D(\d+)\]\(#([^)]+)\)`).FindAllStringSubmatch(plan, -1) {
		indexed[m[1]] = true
		if !strings.HasPrefix(m[2], "d"+m[1]+"-") {
			t.Errorf("D%s: the index anchor %q does not point at it", m[1], m[2])
		}
	}
	for n := range decisions(t) {
		if !indexed[n] {
			t.Errorf("D%s is written and is not in the index at the top of docs/PLAN.md", n)
		}
	}
}

func TestEveryTestNameInTheDocsExists(t *testing.T) {
	t.Parallel()
	written := make(map[string]bool)
	for _, path := range repoFiles(t, ".go") {
		for _, m := range regexp.MustCompile(`(?m)^func ((?:Test|Fuzz|Benchmark)[A-Za-z0-9]+)\(`).FindAllStringSubmatch(read(t, path), -1) {
			written[m[1]] = true
		}
	}
	for _, path := range repoFiles(t, ".md") {
		for _, name := range regexp.MustCompile(`\b(?:Test|Fuzz|Benchmark)[A-Z][A-Za-z0-9]*`).FindAllString(read(t, path), -1) {
			// TestMain is the hook that go test calls, not a test.
			if !written[name] && name != "TestMain" {
				t.Errorf("%s: names %s, which no test defines", path, name)
			}
		}
	}
}

// decisions returns the number of every decision in docs/PLAN.md.
func decisions(t *testing.T) map[string]bool {
	t.Helper()
	written := make(map[string]bool)
	for _, m := range regexp.MustCompile(`(?m)^### D(\d+)\.`).FindAllStringSubmatch(read(t, filepath.Join("docs", "PLAN.md")), -1) {
		written[m[1]] = true
	}
	if len(written) == 0 {
		t.Fatal("docs/PLAN.md holds no decision")
	}
	return written
}

// repoFiles returns every file with one of exts, outside the folders whose
// name starts with a dot, except .github.
func repoFiles(t *testing.T, exts ...string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != "." && strings.HasPrefix(d.Name(), ".") && d.Name() != ".github":
			return filepath.SkipDir
		case !d.IsDir() && slices.Contains(exts, filepath.Ext(path)):
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
