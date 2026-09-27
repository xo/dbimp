package dbimp_test

import (
	"encoding/json/jsontext"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

func TestObjectRowsTakeTheOrderOfTheFirstRow(t *testing.T) {
	t.Parallel()
	dec := jsontext.NewDecoder(strings.NewReader(`[{"b":1,"a":null},{"a":2},{"b":3,"a":4}]`))
	r, err := dbimp.NewObjectRows(dec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cols := r.Columns(); !slices.Equal(cols, []string{"b", "a"}) {
		t.Fatalf("columns are %q, want [b a]", cols)
	}
	want := [][]string{{"1", "null"}, {"", "2"}, {"3", "4"}}
	vals := make([]jsontext.Value, 2)
	for i, row := range want {
		if err := r.Next(vals); err != nil {
			t.Fatalf("reading row %d: %v", i, err)
		}
		for j, v := range row {
			if string(vals[j]) != v {
				t.Errorf("row %d column %d is %q, want %q", i, j, vals[j], v)
			}
		}
	}
	if err := r.Next(vals); !errors.Is(err, io.EOF) {
		t.Errorf("the end of the rows is %v, want io.EOF", err)
	}
}

func TestObjectRowsRefuseANewColumn(t *testing.T) {
	t.Parallel()
	dec := jsontext.NewDecoder(strings.NewReader(`[{"a":1},{"a":2,"b":3}]`))
	r, err := dbimp.NewObjectRows(dec, nil)
	if err != nil {
		t.Fatal(err)
	}
	vals := make([]jsontext.Value, 1)
	if err := r.Next(vals); err != nil {
		t.Fatal(err)
	}
	if err := r.Next(vals); !errors.Is(err, dbimp.ErrExtraColumn) {
		t.Errorf("a row with a new column gave %v, want ErrExtraColumn", err)
	}
}

func TestObjectRowsWithNamedColumns(t *testing.T) {
	t.Parallel()
	dec := jsontext.NewDecoder(strings.NewReader(`[{"a":1,"b":2}]`))
	r, err := dbimp.NewObjectRows(dec, []string{"b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	vals := make([]jsontext.Value, 2)
	if err := r.Next(vals); err != nil {
		t.Fatal(err)
	}
	if string(vals[0]) != "2" || string(vals[1]) != "1" {
		t.Errorf("the row is %q, want [2 1]", vals)
	}
}

func TestObjectRowsEmpty(t *testing.T) {
	t.Parallel()
	r, err := dbimp.NewObjectRows(jsontext.NewDecoder(strings.NewReader(`[]`)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Next(nil); !errors.Is(err, io.EOF) {
		t.Errorf("an empty result gave %v, want io.EOF", err)
	}
}

func TestArrayRows(t *testing.T) {
	t.Parallel()
	r, err := dbimp.NewArrayRows(jsontext.NewDecoder(strings.NewReader(`[[1,"x"],[2,null]]`)), 2)
	if err != nil {
		t.Fatal(err)
	}
	vals := make([]jsontext.Value, 2)
	for range 2 {
		if err := r.Next(vals); err != nil {
			t.Fatal(err)
		}
	}
	if string(vals[1]) != "null" {
		t.Errorf("the last value is %q, want null", vals[1])
	}
	if err := r.Next(vals); !errors.Is(err, io.EOF) {
		t.Errorf("the end of the rows is %v, want io.EOF", err)
	}
}

func TestArrayRowsRefuseTheWrongCount(t *testing.T) {
	t.Parallel()
	r, err := dbimp.NewArrayRows(jsontext.NewDecoder(strings.NewReader(`[[1,2,3]]`)), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Next(make([]jsontext.Value, 2)); !errors.Is(err, dbimp.ErrColumnCount) {
		t.Errorf("a row with three values gave %v, want ErrColumnCount", err)
	}
}
