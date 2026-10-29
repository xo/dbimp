package maxcompute //nolint:testpackage // The tests read the answers of the server, which the package reads.

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadAnswerOfAPoll(t *testing.T) {
	t.Parallel()
	f := recordedFlow(t, "SELECT 1 AS a, 'x' AS b", "Success")
	a, err := readAnswer(strings.NewReader(string(f.status.Response.Content())))
	if err != nil {
		t.Fatal(err)
	}
	if a.status != "Success" || a.content != nil {
		t.Errorf("the answer is %+v, want the state of the task, which follows the state of the instance, and no result", a)
	}
}

func TestReadAnswerOfAResult(t *testing.T) {
	t.Parallel()
	f := recordedFlow(t, "SELECT 1 AS a, 'x' AS b", "Success")
	a, err := readAnswer(strings.NewReader(string(f.result.Response.Content())))
	if err != nil {
		t.Fatal(err)
	}
	if a.status != "Success" || a.format != "csv" || a.transform != "" {
		t.Errorf("the answer is %+v", a)
	}
	text, err := a.readText()
	if err != nil || text != "\"a\",\"b\"\r\n1,\"x\"\r\n" {
		t.Errorf("the text is %q, %v", text, err)
	}
}

func TestReadAnswerOfAResultInBase64(t *testing.T) {
	t.Parallel()
	text := resultOf(t, "'a,\"b\"\nc é 日本 😀 \\N' AS lo")
	recs := cells(t, text)
	if len(recs) != 2 || len(recs[1]) != 3 {
		t.Fatalf("the result has %d records", len(recs))
	}
	if got := recs[1][0].text; got != "a,\"b\"\nc é 日本 😀 N" {
		t.Errorf("the string is %q", got)
	}
	if recs[1][1].text != "" || !recs[1][2].null {
		t.Errorf("the empty string and the NULL are %+v and %+v", recs[1][1], recs[1][2])
	}
}

func TestReadAnswerOfAFailedTask(t *testing.T) {
	t.Parallel()
	f := recordedFlow(t, "SELECT * FROM dbimp_it_missing", "Failed")
	task, err := newTask(io.NopCloser(strings.NewReader(string(f.result.Response.Content()))))
	if task != nil {
		t.Error("a task that failed gave a result")
	}
	e, ok := errors.AsType[*Error](err)
	if !ok || e.state != stateFailed || !strings.HasPrefix(e.Message, "ODPS-0130131") {
		t.Errorf("the error is %v", err)
	}
}

func TestAnAnswerThatIsCutIsAnError(t *testing.T) {
	t.Parallel()
	f := recordedFlow(t, "SELECT 1 AS a, 'x' AS b", "Success")
	head, _ := resultParts(t, f.result)
	a, err := readAnswer(strings.NewReader(head + "\"a\"\r\n1\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.readText(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a result that ends inside its data gave %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestCSVCells(t *testing.T) {
	t.Parallel()
	recs := cells(t, "\"a\",\"b\"\r\n1,\"x\"\"y\"\r\n,\"\"\r\n\"\\N\",\"\"\\N\"\"\r\n\"last\"")
	want := [][]cell{
		{{text: "a", quoted: true}, {text: "b", quoted: true}},
		{{text: "1"}, {text: `x"y`, quoted: true}},
		{{text: ""}, {text: "", quoted: true}},
		{{text: `\N`, null: true, quoted: true}, {text: `\N`, quoted: true}},
		{{text: "last", quoted: true}},
	}
	if len(recs) != len(want) {
		t.Fatalf("%d records, want %d: %+v", len(recs), len(want), recs)
	}
	for i := range want {
		for j := range want[i] {
			if recs[i][j] != want[i][j] {
				t.Errorf("the cell %d of the record %d is %+v, want %+v", j, i, recs[i][j], want[i][j])
			}
		}
	}
}

func TestCSVThatIsCutIsAnError(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"\"abc", "\"a\"x"} {
		r := newCSVReader(strings.NewReader(in))
		if _, err := r.record(); err == nil || errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("record of %q gave %v, want an error", in, err)
		}
	}
}
