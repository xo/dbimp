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
//
// A product with a second server, such as the Controller of Pinot, names its
// URL with -second, and a request of the setup or the teardown goes there
// when it says "server": "second".
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
	// Auth is "bearer" to send the password of each URL as a Bearer token,
	// as for a server that takes a token and no user, such as libSQL with a
	// JWT (D94). It is "sigv4" to sign each request with AWS Signature
	// Version 4, as DynamoDB takes it, with the user of each URL as the
	// access key and its password as the secret key. It is "" for basic
	// authentication.
	Auth string `json:"auth,omitzero"`
	// Region and Service name the scope of each signature when Auth is
	// "sigv4", such as "us-east-1" and "dynamodb".
	Region  string `json:"region,omitzero"`
	Service string `json:"service,omitzero"`
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
	// Releases limits the request to the releases whose name starts with one
	// of these, such as "influxdb-3". It is empty for every release.
	Releases []string `json:"releases,omitzero"`
	// Phase is "setup" for a request that runs once, as the administrator,
	// before the requests of both principals, "teardown" for one that runs
	// once, as the administrator, after them, or "" for a request that each
	// principal sends.
	Phase string `json:"phase,omitzero"`
	// Capture names values of the response to keep, by a path such as
	// "results.0.txid". A later request of the same principal writes a kept
	// value into its body, its path or a header as {{name}}. In a body, the
	// value is escaped as the text of a JSON string.
	Capture map[string]string `json:"capture,omitzero"`
	// Follow names a URI of the response, by a path such as "next_uri". The
	// command then sends GET to it, with the same headers, and records the
	// answer, until an answer has no such URI, as a client that reads every
	// page does. It follows at most maxFollow times.
	Follow string `json:"follow,omitzero"`
	// Server is "second" to send the request to the URL of the flag -second,
	// such as the Controller of Pinot, with the credentials of that URL, or
	// "" to send it to the URL of the principal. Only a request of the setup
	// or the teardown can name it.
	Server string `json:"server,omitzero"`

	// auth, region and service are the Auth, the Region and the Service of
	// the script.
	auth, region, service string
}

// maxFollow bounds the pages that one request follows.
const maxFollow = 1000

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
	ordinary := flag.String("ordinary", "", "the http URL of the server, with the user and password of the ordinary user, or empty for a release that has none")
	second := flag.String("second", "", "the http URL of a second server of the product, such as the Controller of Pinot, or empty for none")
	flag.Parse()
	if *dir == "" || *release == "" || *admin == "" {
		flag.Usage()
		return errors.New("reading the flags: dir, release and admin are needed")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx, *dir, *release, *second, map[string]string{
		dbimptest.Administrator: *admin,
		dbimptest.Ordinary:      *ordinary,
	})
}

func run(ctx context.Context, dir, release, second string, principals map[string]string) error {
	b, err := os.ReadFile(filepath.Join(dir, dbimptest.RequestsName))
	if err != nil {
		return fmt.Errorf("reading the script: %w", err)
	}
	var script Script
	if err := json.Unmarshal(b, &script, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("reading the script: %w", err)
	}
	switch {
	case !slices.Contains([]string{"", dbimp.AuthBearer, authSigV4}, script.Auth):
		return fmt.Errorf("reading the script: the auth %q: %w", script.Auth, dbimp.ErrInvalidValue)
	case script.Auth == authSigV4 && (script.Region == "" || script.Service == ""):
		return fmt.Errorf("reading the script: the auth %q needs a region and a service: %w", script.Auth, dbimp.ErrInvalidValue)
	}
	for i := range script.Requests {
		r := &script.Requests[i]
		r.auth, r.region, r.service = script.Auth, script.Region, script.Service
	}
	if err := forget(dir, release); err != nil {
		return err
	}
	rec := dbimptest.NewRecorder(dir, release, http.DefaultTransport)
	for _, u := range principals {
		if pu, err := url.Parse(u); err == nil && pu.User != nil {
			pass, _ := pu.User.Password()
			rec.Secret(pass)
		}
	}
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
	servers := map[string]*url.URL{"": admin}
	if second != "" {
		if servers["second"], err = url.Parse(second); err != nil {
			return fmt.Errorf("parsing the URL of the second server: %w", err)
		}
	}
	script.Requests = slices.DeleteFunc(script.Requests, func(r Request) bool {
		return len(r.Releases) > 0 && !slices.ContainsFunc(r.Releases, func(prefix string) bool {
			return strings.HasPrefix(release, prefix)
		})
	})
	if err := runPhase(ctx, client, servers, script, "setup"); err != nil {
		return err
	}
	for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
		// A release with no ordinary user, such as InfluxDB 3 Core, records
		// the administrator only.
		if principals[p] == "" {
			continue
		}
		base, err := url.Parse(principals[p])
		if err != nil {
			return fmt.Errorf("parsing the URL of the %s user: %w", p, err)
		}
		kept := credentials(base)
		for _, r := range script.Requests {
			if r.Phase != "" || len(r.Principals) > 0 && !slices.Contains(r.Principals, p) {
				continue
			}
			if r.Server != "" {
				return fmt.Errorf("reading item %d, %s: only a request of the setup or the teardown can name a server", r.Item, r.Name)
			}
			r.Body = expand(r.Body, kept)
			r.Path = expandText(r.Path, kept)
			r.Header = expandHeader(withHeader(r.Header, script.Header[p]), kept)
			if r.Background {
				wg.Go(func() {
					if err := send(ctx, client, base, p, r, nil); err != nil {
						mu.Lock()
						errs = append(errs, fmt.Errorf("recording item %d, %s, as the %s user: %w", r.Item, r.Name, p, err))
						mu.Unlock()
					}
				})
				continue
			}
			if err := send(ctx, client, base, p, r, kept); err != nil {
				return fmt.Errorf("recording item %d, %s, as the %s user: %w", r.Item, r.Name, p, err)
			}
		}
		wg.Wait()
	}
	if err := runPhase(ctx, client, servers, script, "teardown"); err != nil {
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

// runPhase sends the requests of a phase once, as the administrator, each
// to the server that it names.
func runPhase(ctx context.Context, client *http.Client, servers map[string]*url.URL, script Script, phase string) error {
	// A request of the phase can keep a value for a later one, such as the
	// id of a statement of Avatica, on each server.
	keptOf := map[string]map[string]string{}
	for _, r := range script.Requests {
		if r.Phase != phase {
			continue
		}
		base, ok := servers[r.Server]
		if !ok {
			return fmt.Errorf("running the %s, item %d, %s: no URL for the server %q", phase, r.Item, r.Name, r.Server)
		}
		r.Header = withHeader(r.Header, script.Header[dbimptest.Administrator])
		kept := keptOf[r.Server]
		if kept == nil {
			kept = credentials(base)
			keptOf[r.Server] = kept
		}
		r.Body = expand(r.Body, kept)
		r.Path = expandText(r.Path, kept)
		if err := send(ctx, client, base, dbimptest.Administrator, r, kept); err != nil {
			return fmt.Errorf("running the %s, item %d, %s: %w", phase, r.Item, r.Name, err)
		}
	}
	return nil
}

// credentials returns the kept values that a request starts with: {{user}}
// and {{password}}, the credentials of the URL of the principal, for a server
// that takes them in the body, such as Avatica. The Recorder writes the
// password as REDACTED.
func credentials(base *url.URL) map[string]string {
	kept := map[string]string{}
	if base.User != nil {
		kept["user"] = base.User.Username()
		kept["password"], _ = base.User.Password()
	}
	return kept
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

// expandText writes each kept value into s where {{name}} stands.
func expandText(s string, kept map[string]string) string {
	for name, v := range kept {
		s = strings.ReplaceAll(s, "{{"+name+"}}", v)
	}
	return s
}

// expandHeader writes each kept value into the values of h where {{name}}
// stands, such as the id of a transaction in a header.
func expandHeader(h http.Header, kept map[string]string) http.Header {
	if len(h) == 0 || len(kept) == 0 {
		return h
	}
	out := make(http.Header, len(h))
	for key, vals := range h {
		for _, v := range vals {
			out.Add(key, expandText(v, kept))
		}
	}
	return out
}

// expand writes each kept value into body where {{name}} stands, escaped as
// the text of a JSON string, so that a value with a quote, such as the
// session of Databend, stays valid JSON.
func expand(body jsontext.Value, kept map[string]string) jsontext.Value {
	if len(body) == 0 || len(kept) == 0 {
		return body
	}
	s := string(body)
	for name, v := range kept {
		// A kept object or array takes the place of the whole string that
		// names it, such as "{{handle}}", as JSON.
		if raw, ok := strings.CutPrefix(v, rawPrefix); ok {
			s = strings.ReplaceAll(s, `"{{`+name+`}}"`, raw)
			continue
		}
		q, err := jsontext.AppendQuote(nil, v)
		if err != nil {
			q = []byte(`"` + v + `"`)
		}
		s = strings.ReplaceAll(s, "{{"+name+"}}", string(q[1:len(q)-1]))
	}
	return jsontext.Value(s)
}

// rawPrefix starts a kept value that is JSON, an object or an array, which
// expand writes in place of the string that names it.
const rawPrefix = "\x00json:"

// headerPrefix starts a path of a capture that names a header of the
// response, such as "header:X-Trino-Started-Transaction-Id", and not a value
// of its body.
const headerPrefix = "header:"

// captureHeaders keeps the headers of h that a capture names, as
// "header:Name". A header that the response does not have is not kept.
func captureHeaders(h http.Header, paths map[string]string, kept map[string]string) {
	for name, path := range paths {
		if hn, ok := strings.CutPrefix(path, headerPrefix); ok {
			if v := h.Get(hn); v != "" {
				kept[name] = v
			}
		}
	}
}

// hostless returns the path and the query of uri when it is an absolute URL,
// such as the nextUri that Trino sends, because the script names the server
// once and each request goes to it. Any other uri is returned as it is.
func hostless(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Host != "" {
		return u.RequestURI()
	}
	return uri
}

// capture keeps the values of body that the request names. A response with
// no body keeps none, and a member that the body does not have is not kept,
// as the last page of Trino has no nextUri. The request that uses a value
// that was not kept then fails, which shows the mistake.
func capture(body []byte, paths map[string]string, kept map[string]string) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return fmt.Errorf("reading the response to capture: %w", err)
	}
	for name, path := range paths {
		if strings.HasPrefix(path, headerPrefix) {
			continue
		}
		cur, err := lookup(v, path)
		if err != nil {
			return fmt.Errorf("capturing %s: %w", name, err)
		}
		if cur == nil {
			fmt.Printf("capturing %s: the response has nothing at %s\n", name, path)
			continue
		}
		switch c := cur.(type) {
		case string:
			kept[name] = c
		case float64:
			// A number, such as the statementId of Avatica, is kept as its
			// text, which a server such as Avatica reads back from a string.
			kept[name] = strconv.FormatFloat(c, 'f', -1, 64)
		case map[string]any, []any:
			// An object, such as the statement handle of Avatica, is kept as
			// its JSON.
			b, err := json.Marshal(c)
			if err != nil {
				return fmt.Errorf("capturing %s: %w", name, err)
			}
			kept[name] = rawPrefix + string(b)
		default:
			return fmt.Errorf("capturing %s: %s is not a string, a number or an object", name, path)
		}
	}
	return nil
}

// lookup returns the value of v at path, such as "results.0.txid", or nil
// when an object has no such member.
func lookup(v any, path string) (any, error) {
	cur := v
	for part := range strings.SplitSeq(path, ".") {
		switch c := cur.(type) {
		case map[string]any:
			cur = c[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i >= len(c) {
				return nil, fmt.Errorf("no element %s", part)
			}
			cur = c[i]
		default:
			return nil, fmt.Errorf("nothing at %s", part)
		}
	}
	return cur, nil
}

// next returns the URI of body at the path follow, or "" when the body has
// none.
func next(body []byte, follow string) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	cur, err := lookup(v, follow)
	if err != nil {
		return ""
	}
	s, _ := cur.(string)
	return s
}

// send sends one request. A request with a timeout is expected to fail.
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
	path, query, _ := strings.Cut(hostless(r.Path), "?")
	u.Path, u.RawQuery = path, query
	// A signature of AWS covers the bytes of the body, so the body is
	// built before the request.
	var payload []byte
	switch {
	case r.Text != "":
		payload = []byte(r.Text)
	case len(r.Body) > 0 && r.Encoding == "cbor":
		b, err := toCBOR(r.Body)
		if err != nil {
			return fmt.Errorf("encoding the body as CBOR: %w", err)
		}
		payload = b
	case len(r.Body) > 0:
		v := r.Body.Clone()
		if err := v.Compact(); err != nil {
			return fmt.Errorf("compacting the body: %w", err)
		}
		payload = v
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u.String(), body)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
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
		switch r.auth {
		case dbimp.AuthBearer:
			req.Header.Set("Authorization", "Bearer "+pass)
		case authSigV4:
			dbimp.SignV4(req, payload, user.Username(), pass, r.region, r.service, time.Now())
		default:
			req.SetBasicAuth(user.Username(), pass)
		}
	}
	res, err := client.Do(req)
	switch {
	case err != nil && r.Timeout != "":
		fmt.Printf("item %d, %s: the client gave up after %s, as planned\n", r.Item, r.Name, r.Timeout)
		return nil
	case err != nil:
		return fmt.Errorf("sending the request: %w", err)
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		fmt.Printf("item %d, %s: %s, and the server closed the body early\n", r.Item, r.Name, res.Status)
		return nil
	case err != nil:
		return fmt.Errorf("reading the response: %w", err)
	}
	if len(r.Capture) > 0 && kept != nil {
		captureHeaders(res.Header, r.Capture, kept)
		if err := capture(resBody, r.Capture, kept); err != nil {
			return err
		}
	}
	fmt.Printf("item %d, %s: %s\n", r.Item, r.Name, res.Status)
	if r.Follow == "" {
		return nil
	}
	for range maxFollow {
		uri := next(resBody, r.Follow)
		if uri == "" {
			return nil
		}
		var hdr http.Header
		if resBody, hdr, err = get(ctx, client, base, r, uri); err != nil {
			return fmt.Errorf("following %s: %w", uri, err)
		}
		if len(r.Capture) > 0 && kept != nil {
			captureHeaders(hdr, r.Capture, kept)
		}
	}
	return fmt.Errorf("following %s: more than %d pages", r.Follow, maxFollow)
}

// authSigV4 is the Auth of a script that signs each request with AWS
// Signature Version 4.
const authSigV4 = "sigv4"

// get sends GET to the URI uri of a page that r follows, with the headers
// and the credentials of r, and returns the body of the answer, which the
// transport records.
func get(ctx context.Context, client *http.Client, base *url.URL, r Request, uri string) ([]byte, http.Header, error) {
	u := *base
	u.User = nil
	path, query, _ := strings.Cut(hostless(uri), "?")
	u.Path, u.RawQuery = path, query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("building the request: %w", err)
	}
	for key, vals := range r.Header {
		if key == "Content-Type" {
			continue
		}
		for _, val := range vals {
			req.Header.Add(key, val)
		}
	}
	if user := base.User; user != nil {
		pass, _ := user.Password()
		req.SetBasicAuth(user.Username(), pass)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("sending the request: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading the response: %w", err)
	}
	fmt.Printf("item %d, %s: GET %s: %s\n", r.Item, r.Name, path, res.Status)
	return b, res.Header, nil
}

// forget removes the files and the entries that an earlier run wrote for
// release.
func forget(dir, release string) error {
	paths, err := filepath.Glob(filepath.Join(dir, release+"-*.json"))
	if err != nil {
		return fmt.Errorf("finding the recordings of %s: %w", release, err)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing a recording: %w", err)
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
