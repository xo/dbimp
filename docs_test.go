package dbimp_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// These tests read the repository rather than the package. They hold the
// layout of D3, D71 and D72: four documents at the root, every other one in
// docs/, every one of those named in the tables in AGENTS.md and README.md,
// and each decision in a file of its own in docs/decisions/.

// rootDocs are the only Markdown files at the root (D71).
var rootDocs = []string{"AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md", "README.md"}

// decisionFile matches the name of the file of a decision, such as
// D071-every-xo-repository.md (D72).
var decisionFile = regexp.MustCompile(`^D(\d{3})-[a-z0-9-]+\.md$`)

// markdownLink matches a relative link to a Markdown file.
var markdownLink = regexp.MustCompile(`\]\((?:\./)?([^)#:]+\.md)(#[^)]*)?\)`)

// bareMention matches a document named in running text, which is how a Go
// comment or a workflow comment points at one, as in docs/BACKLOG.md.
var bareMention = regexp.MustCompile(`(?:^|[\s` + "`" + `(])((?:docs/)?[A-Z][A-Z_]*\.md)`)

// decisionRef matches a reference to a decision, such as D17.
var decisionRef = regexp.MustCompile(`\bD([1-9][0-9]{0,2})\b`)

// otherRepo matches the name of a sibling repository before a decision
// number, as in "dbmeta D62". Such a reference names a decision of that
// repository, not of this one.
var otherRepo = regexp.MustCompile("(?:cql|dbmeta|dburl|n1ql|tblfmt|usql)[^\\s]*`?[ (]*$")

func TestTheRootHoldsFourDocuments(t *testing.T) {
	t.Parallel()
	matches, err := filepath.Glob("*.md")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(matches, rootDocs) {
		t.Errorf("the root holds %v, want %v. Every other document goes in docs/ (D3 and D71)", matches, rootDocs)
	}
}

func TestClaudeImportsAgents(t *testing.T) {
	t.Parallel()
	info, err := os.Lstat("CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("CLAUDE.md is a symbolic link. Make it a file that holds @AGENTS.md (D71)")
	}
	if got := strings.TrimSpace(read(t, "CLAUDE.md")); got != "@AGENTS.md" {
		t.Errorf("CLAUDE.md holds %q. It holds only @AGENTS.md, and the rules go in AGENTS.md (D71)", got)
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
	docs = append(docs, filepath.Join("docs", "decisions", "README.md"))
	for _, table := range []string{"AGENTS.md", "README.md"} {
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
	written := make(map[string]bool)
	for _, d := range decisions(t) {
		written[d.num] = true
	}
	for _, path := range repoFiles(t, ".md", ".go", ".yml") {
		body := read(t, path)
		for _, m := range decisionRef.FindAllStringSubmatchIndex(body, -1) {
			if otherRepo.MatchString(body[max(0, m[0]-24):m[0]]) {
				continue
			}
			if n := body[m[2]:m[3]]; !written[n] {
				t.Errorf("%s: refers to D%s, which is not in docs/decisions/", path, n)
			}
		}
	}
}

func TestTheDecisionIndexIsComplete(t *testing.T) {
	t.Parallel()
	index := read(t, filepath.Join("docs", "decisions", "README.md"))
	rows := make(map[string]string)
	for _, m := range regexp.MustCompile(`(?m)^\| \[D(\d+)\]\(.*$`).FindAllStringSubmatch(index, -1) {
		rows[m[1]] = m[0]
	}
	written := make(map[string]bool)
	for _, d := range decisions(t) {
		written[d.num] = true
		want := fmt.Sprintf("| [D%s](%s) | %s | %s |", d.num, d.file, d.title, d.status)
		switch got, ok := rows[d.num]; {
		case !ok:
			t.Errorf("D%s has no row in docs/decisions/README.md. Add:\n%s", d.num, want)
		case got != want:
			t.Errorf("D%s: the row in docs/decisions/README.md is\n%s\nand the file says\n%s", d.num, got, want)
		}
	}
	for num := range rows {
		if !written[num] {
			t.Errorf("docs/decisions/README.md has a row for D%s, and no file holds it", num)
		}
	}
}

func TestAnAmendmentPointsBothWays(t *testing.T) {
	t.Parallel()
	status := make(map[string]string)
	for _, d := range decisions(t) {
		status[d.num] = d.title + ". " + d.status
	}
	// "Amends D65" and "Amended by D69" both name another decision, and that
	// decision has to name this one back.
	naming := regexp.MustCompile(`(?i)\b(?:amends|supersedes|superseded by|replaced by|amended by) D(\d+)`)
	for num, head := range status {
		for _, m := range naming.FindAllStringSubmatch(head, -1) {
			other := m[1]
			if _, ok := status[other]; !ok {
				t.Errorf("D%s names D%s, which is not a decision", num, other)
				continue
			}
			if !strings.Contains(status[other], "D"+num) {
				t.Errorf("D%s says %q, and D%s does not name D%s. An amendment is named from both "+
					"sides, or a reader who finds the older decision gets a rule that no longer holds (D72)",
					num, head, other, num)
			}
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

// decision is one decision in docs/decisions/.
type decision struct {
	num, title, status, file string
}

// decisions reads every decision in docs/decisions/, in order (D72).
func decisions(t *testing.T) []decision {
	t.Helper()
	dir := filepath.Join("docs", "decisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	head := regexp.MustCompile(`\A# D(\d+)\. (.+)\n\nStatus: (.+)\.\n`)
	var out []decision
	for _, e := range entries {
		if e.Name() == "README.md" {
			continue
		}
		name := decisionFile.FindStringSubmatch(e.Name())
		if name == nil {
			t.Errorf("%s: a decision file is named D, three digits, a hyphen and the title in"+
				" lower case words, such as D071-every-xo-repository.md (D72)", e.Name())
			continue
		}
		m := head.FindStringSubmatch(read(t, filepath.Join(dir, e.Name())))
		if m == nil {
			t.Errorf("%s: a decision opens with \"# D<n>. <title>\", a blank line and \"Status: <status>.\" (D72)", e.Name())
			continue
		}
		if n, _ := strconv.Atoi(name[1]); strconv.Itoa(n) != m[1] {
			t.Errorf("%s holds D%s. The file name and the heading name one decision (D72)", e.Name(), m[1])
		}
		out = append(out, decision{num: m[1], title: m[2], status: m[3], file: e.Name()})
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no decision", dir)
	}
	return out
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
