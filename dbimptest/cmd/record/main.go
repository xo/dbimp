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
	"strings"
	"sync"
	"time"

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
	for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
		base, err := url.Parse(principals[p])
		if err != nil {
			return fmt.Errorf("parsing the URL of the %s user: %w", p, err)
		}
		for _, r := range script.Requests {
			if len(r.Principals) > 0 && !slices.Contains(r.Principals, p) {
				continue
			}
			if r.Background {
				wg.Go(func() {
					if err := send(ctx, client, base, p, r); err != nil {
						mu.Lock()
						errs = append(errs, fmt.Errorf("item %d, %s, as the %s user: %w", r.Item, r.Name, p, err))
						mu.Unlock()
					}
				})
				continue
			}
			if err := send(ctx, client, base, p, r); err != nil {
				return fmt.Errorf("item %d, %s, as the %s user: %w", r.Item, r.Name, p, err)
			}
		}
		wg.Wait()
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
func send(ctx context.Context, client *http.Client, base *url.URL, p string, r Request) error {
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
	if len(r.Body) > 0 {
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
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return err
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
