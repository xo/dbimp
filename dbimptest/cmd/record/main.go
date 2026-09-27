// Command record sends the requests of step 6 of docs/DRIVER.md to a real
// server, as the administrator and as the ordinary user, and writes each
// exchange and the manifest into testdata/<driver>/.
//
// It reads the requests from requests.json in that folder. It replaces the
// files and the entries of the manifest that an earlier run wrote for the
// same release, so the folder holds one recording of each release.
//
//	go run ./dbimptest/cmd/record -dir testdata/couchbase -release couchbase-8.0.3 \
//		-admin http://Administrator:pass@127.0.0.1:55059 \
//		-ordinary http://dbmeta_user:pass@127.0.0.1:55059
package main

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// Script is the content of requests.json.
type Script struct {
	// Driver is the name of the driver.
	Driver string `json:"driver"`
	// Requests are the requests, in the order to send them.
	Requests []Request `json:"requests"`
	// Absent lists the items of step 6 that do not apply to the product.
	Absent []Absent `json:"absent,omitzero"`
	// Header holds, by the name of each principal, the headers that every
	// request of that principal sends, such as the headers that say where a
	// SurrealDB user is defined.
	Header map[string]http.Header `json:"header,omitzero"`
}

// Request is one request of the script.
type Request struct {
	// Item is the number of the item in step 6.
	Item int `json:"item"`
	// Name says what the request measures.
	Name string `json:"name"`
	// Method is the HTTP method.
	Method string `json:"method"`
	// Path is the path, with the query if it has one.
	Path string `json:"path"`
	// Header holds the headers to send.
	Header http.Header `json:"header,omitzero"`
	// Body is the JSON body to send, if any.
	Body jsontext.Value `json:"body,omitzero"`
	// Text is a body of plain text to send, in place of Body.
	Text string `json:"text,omitzero"`
	// Encoding is "cbor" to send Body as CBOR, or "" to send it as JSON.
	Encoding string `json:"encoding,omitzero"`
	// Auth is "wrong" to send a wrong password, or "" for the right one.
	Auth string `json:"auth,omitzero"`
	// Timeout makes the client give up after it. The request is then not
	// recorded, and a later request records what the server did.
	Timeout string `json:"timeout,omitzero"`
	// Wait is how long to wait before the request.
	Wait string `json:"wait,omitzero"`
	// Background sends the request without waiting for its response, so
	// that a later request can act on it while it runs. The run waits for
	// it before it writes the manifest.
	Background bool `json:"background,omitzero"`
	// Principals limits the request to these principals. It is empty for
	// both.
	Principals []string `json:"principals,omitzero"`
	// Phase is "setup" for a request that runs once, as the administrator,
	// before the requests of both principals, "teardown" for one that runs
	// once, as the administrator, after them, or "" for a request that each
	// principal sends.
	Phase string `json:"phase,omitzero"`
	// Capture names values of the response to keep, by a path such as
	// "results.0.txid". A later request of the same principal writes a kept
	// value into its body as {{name}}.
	Capture map[string]string `json:"capture,omitzero"`
}

// Absent is an item of step 6 that does not apply to the product.
type Absent struct {
	Item   int    `json:"item"`
	Reason string `json:"reason"`
}

func main() {
	if err := record(); err != nil {
		fmt.Fprintln(os.Stderr, "record:", err)
		os.Exit(1)
	}
}

// record reads the flags and runs the script, and owns the context of the
// program.
func record() error {
	dir := flag.String("dir", "", "the folder under testdata that holds requests.json")
	release := flag.String("release", "", "the release, as dbrun names it")
	admin := flag.String("admin", "", "the http URL of the server, with the user and password of the administrator")
	ordinary := flag.String("ordinary", "", "the http URL of the server, with the user and password of the ordinary user")
	flag.Parse()
	if *dir == "" || *release == "" || *admin == "" || *ordinary == "" {
		flag.Usage()
		return errors.New("every flag is needed")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx, *dir, *release, map[string]string{
		dbimptest.Administrator: *admin,
		dbimptest.Ordinary:      *ordinary,
	})
}

func run(ctx context.Context, dir, release string, principals map[string]string) error {
	b, err := os.ReadFile(filepath.Join(dir, dbimptest.RequestsName))
	if err != nil {
		return fmt.Errorf("reading the script: %w", err)
	}
	var script Script
	if err := json.Unmarshal(b, &script, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("reading the script: %w", err)
	}
	if err := forget(dir, release); err != nil {
		return err
	}
	rec := dbimptest.NewRecorder(dir, release, http.DefaultTransport)
	client := &http.Client{
		Transport: rec,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	admin, err := url.Parse(principals[dbimptest.Administrator])
	if err != nil {
		return fmt.Errorf("parsing the URL of the administrator: %w", err)
	}
	if err := runPhase(ctx, client, admin, script, "setup"); err != nil {
		return err
	}
	for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
		base, err := url.Parse(principals[p])
		if err != nil {
			return fmt.Errorf("parsing the URL of the %s user: %w", p, err)
		}
		kept := map[string]string{}
		for _, r := range script.Requests {
			if r.Phase != "" || len(r.Principals) > 0 && !slices.Contains(r.Principals, p) {
				continue
			}
			r.Body = expand(r.Body, kept)
			r.Header = withHeader(r.Header, script.Header[p])
			if r.Background {
				wg.Go(func() {
					if err := send(ctx, client, base, p, r, nil); err != nil {
						mu.Lock()
						errs = append(errs, fmt.Errorf("item %d, %s, as the %s user: %w", r.Item, r.Name, p, err))
						mu.Unlock()
					}
				})
				continue
			}
			if err := send(ctx, client, base, p, r, kept); err != nil {
				return fmt.Errorf("item %d, %s, as the %s user: %w", r.Item, r.Name, p, err)
			}
		}
		wg.Wait()
	}
	if err := runPhase(ctx, client, admin, script, "teardown"); err != nil {
		return err
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := rec.WriteManifest(script.Driver); err != nil {
		return err
	}
	return addAbsent(dir, release, script.Absent)
}

// send sends one request. A request with a timeout is expected to fail.
// runPhase sends the requests of a phase once, as the administrator.
func runPhase(ctx context.Context, client *http.Client, admin *url.URL, script Script, phase string) error {
	for _, r := range script.Requests {
		if r.Phase != phase {
			continue
		}
		r.Header = withHeader(r.Header, script.Header[dbimptest.Administrator])
		if err := send(ctx, client, admin, dbimptest.Administrator, r, nil); err != nil {
			return fmt.Errorf("the %s, item %d, %s: %w", phase, r.Item, r.Name, err)
		}
	}
	return nil
}

// withHeader returns h with the headers of extra added.
func withHeader(h, extra http.Header) http.Header {
	if len(extra) == 0 {
		return h
	}
	h = h.Clone()
	if h == nil {
		h = http.Header{}
	}
	for key, vals := range extra {
		for _, val := range vals {
			h.Add(key, val)
		}
	}
	return h
}

// toCBOR returns the JSON value v as CBOR. An object is a map, an array is an
// array, a number with no fraction and no exponent is an integer, and any
// other number is a float64.
func toCBOR(v jsontext.Value) ([]byte, error) {
	var e dbimp.CBOREncoder
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if err := jsonToCBOR(&e, dec); err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

func jsonToCBOR(e *dbimp.CBOREncoder, dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case '{', '[':
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		end := jsontext.EndObject
		if tok.Kind() == '[' {
			end = jsontext.EndArray
		}
		var inner dbimp.CBOREncoder
		n := 0
		for dec.PeekKind() != end.Kind() {
			if err := jsonToCBOR(&inner, dec); err != nil {
				return err
			}
			n++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		if tok.Kind() == '{' {
			e.Map(n / 2)
		} else {
			e.Array(n)
		}
		e.Raw(inner.Bytes())
		return nil
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case 'n':
		e.Null()
	case 't', 'f':
		e.Bool(tok.Bool())
	case '"':
		e.Text(tok.String())
	case '0':
		if i, err := strconv.ParseInt(tok.String(), 10, 64); err == nil {
			e.Int(i)
			break
		}
		f, err := strconv.ParseFloat(tok.String(), 64)
		if err != nil {
			return err
		}
		e.Float(f)
	}
	return nil
}

// expand writes each kept value into body where {{name}} stands.
func expand(body jsontext.Value, kept map[string]string) jsontext.Value {
	if len(body) == 0 || len(kept) == 0 {
		return body
	}
	s := string(body)
	for name, v := range kept {
		s = strings.ReplaceAll(s, "{{"+name+"}}", v)
	}
	return jsontext.Value(s)
}

// capture keeps the values of body that the request names.
func capture(body []byte, paths map[string]string, kept map[string]string) error {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Errorf("reading the response to capture: %w", err)
	}
	for name, path := range paths {
		cur := v
		for part := range strings.SplitSeq(path, ".") {
			switch c := cur.(type) {
			case map[string]any:
				cur = c[part]
			case []any:
				i, err := strconv.Atoi(part)
				if err != nil || i >= len(c) {
					return fmt.Errorf("capturing %s: no element %s", name, part)
				}
				cur = c[i]
			default:
				return fmt.Errorf("capturing %s: nothing at %s", name, part)
			}
		}
		s, ok := cur.(string)
		if !ok {
			return fmt.Errorf("capturing %s: %s is not a string", name, path)
		}
		kept[name] = s
	}
	return nil
}

func send(ctx context.Context, client *http.Client, base *url.URL, p string, r Request, kept map[string]string) error {
	if r.Wait != "" {
		d, err := time.ParseDuration(r.Wait)
		if err != nil {
			return fmt.Errorf("parsing the wait: %w", err)
		}
		time.Sleep(d)
	}
	ctx = dbimptest.WithLabel(ctx, r.Item, p)
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			return fmt.Errorf("parsing the timeout: %w", err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	u := *base
	u.User = nil
	path, query, _ := strings.Cut(r.Path, "?")
	u.Path, u.RawQuery = path, query
	var body io.Reader
	switch {
	case r.Text != "":
		body = strings.NewReader(r.Text)
	case len(r.Body) > 0 && r.Encoding == "cbor":
		b, err := toCBOR(r.Body)
		if err != nil {
			return fmt.Errorf("encoding the body as CBOR: %w", err)
		}
		body = bytes.NewReader(b)
	case len(r.Body) > 0:
		v := r.Body.Clone()
		if err := v.Compact(); err != nil {
			return fmt.Errorf("compacting the body: %w", err)
		}
		body = bytes.NewReader(v)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u.String(), body)
	if err != nil {
		return err
	}
	for key, vals := range r.Header {
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}
	if user := base.User; user != nil {
		pass, _ := user.Password()
		if r.Auth == "wrong" {
			pass += "-wrong"
		}
		req.SetBasicAuth(user.Username(), pass)
	}
	res, err := client.Do(req)
	switch {
	case err != nil && r.Timeout != "":
		fmt.Printf("item %d, %s: the client gave up after %s, as planned\n", r.Item, r.Name, r.Timeout)
		return nil
	case err != nil:
		return err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if len(r.Capture) > 0 && kept != nil {
		if err := capture(resBody, r.Capture, kept); err != nil {
			return err
		}
	}
	fmt.Printf("item %d, %s: %s\n", r.Item, r.Name, res.Status)
	return nil
}

// forget removes the files and the entries that an earlier run wrote for
// release.
func forget(dir, release string) error {
	paths, err := filepath.Glob(filepath.Join(dir, release+"-*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, dbimptest.ManifestName)
	m, err := dbimptest.ReadManifest(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	m.Entries = slices.DeleteFunc(m.Entries, func(e dbimptest.Entry) bool {
		return e.Release == release
	})
	return dbimptest.WriteManifest(path, m)
}

// addAbsent adds an entry for each absent item, for each principal.
func addAbsent(dir, release string, absent []Absent) error {
	path := filepath.Join(dir, dbimptest.ManifestName)
	m, err := dbimptest.ReadManifest(path)
	if err != nil {
		return err
	}
	date := time.Now().Format(time.DateOnly)
	for _, a := range absent {
		for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
			m.Entries = append(m.Entries, dbimptest.Entry{
				Item:      a.Item,
				Principal: p,
				Release:   release,
				Date:      date,
				Absent:    a.Reason,
			})
		}
	}
	return dbimptest.WriteManifest(path, m)
}
