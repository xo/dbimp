package influxdb //nolint:testpackage // These tests read the parser of INSERT, which is not exported.

import (
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func TestParseInsert(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query, db, rp, body string
	}{
		{"INSERT m v=1i", "", "", "m v=1i"},
		{"  insert\tm,host=a v=1i 1700000000000000000", "", "", "m,host=a v=1i 1700000000000000000"},
		{"INSERT INTO mydb m v=1i", "mydb", "", "m v=1i"},
		{"INSERT INTO mydb.myrp m v=1i", "mydb", "myrp", "m v=1i"},
		{`INSERT INTO "my db"."my.rp" m v=1i`, "my db", "my.rp", "m v=1i"},
		{`INSERT m\ x,k\=1=a\,b s="a b, c=\"d\"",f=1.5 1`, "", "", `m\ x,k\=1=a\,b s="a b, c=\"d\"",f=1.5 1`},
		{"INSERT m v=1i 1\n# a comment\nm v=2i 2\n", "", "", "m v=1i 1\n# a comment\nm v=2i 2"},
		{"INSERT m s=\"two\nlines\"", "", "", "m s=\"two\nlines\""},
	} {
		ins, err := parseInsert(tt.query)
		if err != nil || ins == nil {
			t.Errorf("parseInsert(%q) = %v, %v", tt.query, ins, err)
			continue
		}
		body, err := ins.body(nil)
		if err != nil {
			t.Errorf("parseInsert(%q): %v", tt.query, err)
			continue
		}
		if ins.db != tt.db || ins.rp != tt.rp || body != tt.body {
			t.Errorf("parseInsert(%q) = %q, %q, %q, want %q, %q, %q", tt.query, ins.db, ins.rp, body, tt.db, tt.rp, tt.body)
		}
	}
	for _, q := range []string{"SELECT 1", "INSERTX m v=1", "INSERT", "insertm v=1", "DELETE FROM m"} {
		if ins, err := parseInsert(q); ins != nil || err != nil {
			t.Errorf("parseInsert(%q) = %v, %v, want no INSERT", q, ins, err)
		}
	}
	for _, q := range []string{
		"INSERT m", "INSERT m,host v=1", "INSERT m v=", "INSERT m s=\"open", "INSERT INTO db", "INSERT INTO a.b.c m v=1",
		"INSERT INTO a. m v=1", "INSERT m v=1 1 2", "INSERT # only a comment",
	} {
		if _, err := parseInsert(q); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseInsert(%q) gave %v, want dbimp.ErrInvalidValue", q, err)
		}
	}
}

func TestInsertBinds(t *testing.T) {
	t.Parallel()
	d, _, err := apd.NewFromString("1.25")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2023, 11, 14, 22, 13, 20, 5, time.UTC)
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  string
	}{
		{"INSERT m,key=$key v=$value", named("key", "a b,c=d", "value", int64(-1)), `m,key=a\ b\,c\=d v=-1i`},
		{"INSERT m v=$1,u=$2,f=$3,b=$4,s=$5,d=$6 $7",
			ordinals(int64(math.MaxInt64), uint64(math.MaxUint64), 1.5, true, `say "hi" \ bye`, d, at),
			`m v=9223372036854775807i,u=18446744073709551615u,f=1.5,b=true,s="say \"hi\" \\ bye",d=1.25 1700000000000000005`},
		// A NULL leaves its tag, its field and its timestamp out (D85).
		{"INSERT m,t=$t v=$v,k=1i $ts", named("t", nil, "v", nil, "ts", nil), "m k=1i"},
		// A placeholder inside a string is text.
		{`INSERT m s="$x",v=$x`, named("x", 1.0), `m s="$x",v=1`},
		{"INSERT m v=$v 1\nm v=$w 2", named("v", int64(1), "w", int64(2)), "m v=1i 1\nm v=2i 2"},
	} {
		ins, err := parseInsert(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ins.body(tt.args)
		if err != nil {
			t.Errorf("%s: %v", tt.query, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s gave\n%s\nwant\n%s", tt.query, got, tt.want)
		}
	}
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"INSERT m v=$v", named("w", int64(1)), dbimp.ErrArguments},
		{"INSERT m v=$v", named("v", int64(1), "w", int64(2)), dbimp.ErrArguments},
		{"INSERT m v=$v", named("v", nil), dbimp.ErrInvalidValue},
		{"INSERT m v=$v", named("v", math.NaN()), dbimp.ErrInvalidValue},
		{"INSERT m v=$v", named("v", []byte("x")), dbimp.ErrNotSupported},
		{"INSERT m,t=$t v=1", named("t", ""), dbimp.ErrInvalidValue},
		{"INSERT m v=1 $t", named("t", "now"), dbimp.ErrNotSupported},
	} {
		ins, err := parseInsert(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ins.body(tt.args); !errors.Is(err, tt.want) {
			t.Errorf("%s with %v gave %v, want %v", tt.query, tt.args, err, tt.want)
		}
	}
}

// named returns named arguments from pairs of a name and a value.
func named(kv ...any) []driver.NamedValue {
	var out []driver.NamedValue
	for i := 0; i+1 < len(kv); i += 2 {
		name, _ := kv[i].(string)
		out = append(out, driver.NamedValue{Name: name, Ordinal: i/2 + 1, Value: kv[i+1]})
	}
	return out
}

// ordinals returns positional arguments.
func ordinals(vs ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(vs))
	for i, v := range vs {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}
