package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// ThriftSpec names the endpoint of a script with Thrift.
type ThriftSpec struct {
	// Path is the path of the endpoint, such as "/cliservice".
	Path string `json:"path"`
	// Version is the protocol version that OpenSession asks for. The default
	// is 9, which is HIVE_CLI_SERVICE_PROTOCOL_V10.
	Version int32 `json:"version,omitzero"`
}

// The states of an operation of HiveServer2 that end it or that never end.
const (
	opFinished = 2
	opCanceled = 3
	opClosed   = 4
	opError    = 5
	opTimedOut = 8
)

// node is a Thrift value that the recorder reads without a schema. The
// recorder never gives a field a meaning except where it keeps a handle.
type node struct {
	t      dbimp.ThriftType
	i      int64
	s      []byte
	fields []nodeField
	elems  []*node
}

type nodeField struct {
	id int16
	v  *node
}

// field returns the struct member with the id, or nil.
func (n *node) field(id int16) *node {
	if n == nil {
		return nil
	}
	for _, f := range n.fields {
		if f.id == id {
			return f.v
		}
	}
	return nil
}

// readNode reads one value of type t.
func readNode(d *dbimp.ThriftDecoder, t dbimp.ThriftType, depth int) (*node, error) {
	if depth > 32 {
		return nil, fmt.Errorf("reading a value nested too deep: %w", dbimp.ErrInvalidValue)
	}
	n := &node{t: t}
	var err error
	switch t {
	case dbimp.ThriftBool:
		var v bool
		if v, err = d.ReadBool(); v {
			n.i = 1
		}
	case dbimp.ThriftI8:
		var v int8
		v, err = d.ReadI8()
		n.i = int64(v)
	case dbimp.ThriftI16:
		var v int16
		v, err = d.ReadI16()
		n.i = int64(v)
	case dbimp.ThriftI32:
		var v int32
		v, err = d.ReadI32()
		n.i = int64(v)
	case dbimp.ThriftI64:
		n.i, err = d.ReadI64()
	case dbimp.ThriftDouble:
		_, err = d.ReadDouble()
	case dbimp.ThriftString:
		n.s, err = d.ReadBinary()
	case dbimp.ThriftStruct:
		for {
			ft, id, ferr := d.ReadField()
			if ferr != nil {
				return nil, ferr
			}
			if ft == dbimp.ThriftStop {
				return n, nil
			}
			v, verr := readNode(d, ft, depth+1)
			if verr != nil {
				return nil, verr
			}
			n.fields = append(n.fields, nodeField{id, v})
		}
	case dbimp.ThriftList, dbimp.ThriftSet:
		et, c, lerr := d.ReadList()
		if lerr != nil {
			return nil, lerr
		}
		for range c {
			v, verr := readNode(d, et, depth+1)
			if verr != nil {
				return nil, verr
			}
			n.elems = append(n.elems, v)
		}
	case dbimp.ThriftMap:
		kt, vt, c, merr := d.ReadMap()
		if merr != nil {
			return nil, merr
		}
		for range c {
			k, kerr := readNode(d, kt, depth+1)
			if kerr != nil {
				return nil, kerr
			}
			v, verr := readNode(d, vt, depth+1)
			if verr != nil {
				return nil, verr
			}
			n.elems = append(n.elems, k, v)
		}
	default:
		return nil, fmt.Errorf("reading a value of type %d: %w", t, dbimp.ErrInvalidValue)
	}
	return n, err
}

// write writes n into e.
func (n *node) write(e *dbimp.ThriftEncoder) {
	switch n.t {
	case dbimp.ThriftBool:
		e.Bool(n.i != 0)
	case dbimp.ThriftI8:
		e.I8(int8(n.i)) //nolint:gosec // G115: the value was an int8.
	case dbimp.ThriftI32:
		e.I32(int32(n.i)) //nolint:gosec // G115: the value was an int32.
	case dbimp.ThriftI64:
		e.I64(n.i)
	case dbimp.ThriftString:
		e.Binary(n.s)
	case dbimp.ThriftStruct:
		for _, f := range n.fields {
			e.Field(f.v.t, f.id)
			f.v.write(e)
		}
		e.Stop()
	}
}

// thriftClient sends the calls of one principal.
type thriftClient struct {
	client *http.Client
	url    string
	user   string
	pass   string
	auth   string
	// hive is true once GetInfo named the server Hive.
	hive bool
}

// call sends one call and returns the result struct of its reply. The
// struct has the HTTP status in status. A reply that is an exception is an
// error.
func (c *thriftClient) call(ctx context.Context, name string, build func(*dbimp.ThriftEncoder)) (*node, int, error) {
	var e dbimp.ThriftEncoder
	e.Message(name, dbimp.ThriftCall, 1)
	e.Field(dbimp.ThriftStruct, 1)
	build(&e)
	e.Stop()
	e.Stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(e.Bytes()))
	if err != nil {
		return nil, 0, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-thrift")
	req.Header.Set("Accept", "application/x-thrift")
	switch c.auth {
	case "none":
	case "wrong":
		req.SetBasicAuth(c.user, "wrong-"+c.pass)
	default:
		req.SetBasicAuth(c.user, c.pass)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("sending %s: %w", name, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, res.StatusCode, nil
	}
	d := dbimp.NewThriftDecoder(res.Body)
	_, mt, _, err := d.ReadMessage()
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("reading the reply to %s: %w", name, err)
	}
	root, err := readNode(d, dbimp.ThriftStruct, 0)
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("reading the reply to %s: %w", name, err)
	}
	if mt == dbimp.ThriftException {
		return nil, res.StatusCode, fmt.Errorf("the server raised an exception for %s: %q", name, root.field(1).s)
	}
	return root.field(0), res.StatusCode, nil
}

// handle writes the handle that was kept.
func handle(h *node) func(*dbimp.ThriftEncoder) {
	return func(e *dbimp.ThriftEncoder) {
		e.Field(dbimp.ThriftStruct, 1)
		h.write(e)
	}
}

// failed reports whether the status of a reply is not a success.
func failed(res *node) bool {
	s := res.field(1).field(1)
	return s == nil || s.i > 1
}

func runThrift(ctx context.Context, rec *dbimptest.Recorder, dir, release string, principals map[string]string, script Script) error {
	spec := script.Thrift
	if spec.Version == 0 {
		spec.Version = 9
	}
	client := &http.Client{Transport: rec}
	for _, phase := range []string{"setup", "", "teardown"} {
		for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
			if principals[p] == "" || (phase != "" && p != dbimptest.Administrator) {
				continue
			}
			base, err := url.Parse(principals[p])
			if err != nil {
				return fmt.Errorf("parsing the URL of the %s user: %w", p, err)
			}
			pass, _ := base.User.Password()
			for _, r := range script.Requests {
				if r.Phase != phase || len(r.Principals) > 0 && !slices.Contains(r.Principals, p) {
					continue
				}
				u := *base
				u.User = nil
				u.Path = spec.Path
				c := &thriftClient{client: client, url: u.String(), user: base.User.Username(), pass: pass, auth: r.Auth}
				rec.Label(r.Item, p)
				if err := runStatement(dbimptest.WithLabel(ctx, r.Item, p), c, spec, r); err != nil {
					return fmt.Errorf("recording item %d, %s, as the %s user: %w", r.Item, r.Name, p, err)
				}
			}
		}
	}
	if err := rec.WriteManifest(script.Driver); err != nil {
		return err
	}
	return addAbsent(dir, release, script.Absent)
}

// runStatement opens a session, runs what the request asks for, and closes it.
func runStatement(ctx context.Context, c *thriftClient, spec *ThriftSpec, r Request) error {
	open, status, err := c.call(ctx, "OpenSession", func(e *dbimp.ThriftEncoder) {
		e.Field(dbimp.ThriftI32, 1)
		e.I32(spec.Version)
		e.Field(dbimp.ThriftString, 2)
		e.String(c.user)
		e.Field(dbimp.ThriftString, 3)
		e.String(c.pass)
	})
	if err != nil {
		return err
	}
	if open == nil {
		fmt.Printf("item %d, %s: OpenSession got HTTP %d\n", r.Item, r.Name, status)
		return nil
	}
	if failed(open) {
		fmt.Printf("item %d, %s: OpenSession was refused\n", r.Item, r.Name)
		return nil
	}
	session := open.field(3)
	// A driver asks the server for its name and its release when it opens a
	// session, so each recording of a session starts as that does.
	for _, kind := range []int32{13, 18} {
		info, _, err := c.call(ctx, "GetInfo", func(e *dbimp.ThriftEncoder) {
			handle(session)(e)
			e.Field(dbimp.ThriftI32, 2)
			e.I32(kind)
		})
		if err != nil {
			return err
		}
		// Hive gets the key that a driver sets for it (D237).
		if kind == 13 && strings.EqualFold(string(info.field(2).field(1).s), "hive") {
			c.hive = true
		}
	}
	defer func() {
		_, _, _ = c.call(ctx, "CloseSession", handle(session))
	}()
	if r.Call != "" {
		return runBadCall(ctx, c, r)
	}
	if r.Catalog != "" {
		return catalog(ctx, c, session, r)
	}
	if r.Statement == "" {
		fmt.Printf("item %d, %s: opened a session\n", r.Item, r.Name)
		return nil
	}
	for _, stmt := range append([]string{r.Statement}, r.Also...) {
		if err := execute(ctx, c, session, r, stmt); err != nil {
			return err
		}
	}
	return nil
}

// execute runs one statement in a session and reads its result.
func execute(ctx context.Context, c *thriftClient, session *node, r Request, stmt string) error {
	ex, _, err := c.call(ctx, "ExecuteStatement", func(e *dbimp.ThriftEncoder) {
		handle(session)(e)
		e.Field(dbimp.ThriftString, 2)
		e.String(stmt)
		if c.hive {
			e.Field(dbimp.ThriftMap, 3)
			e.Map(dbimp.ThriftString, dbimp.ThriftString, 1)
			e.String("hive.resultset.use.unique.column.names")
			e.String("false")
		}
		e.Field(dbimp.ThriftBool, 4)
		e.Bool(true)
	})
	if err != nil {
		return err
	}
	if failed(ex) {
		fmt.Printf("item %d, %s: ExecuteStatement refused the statement\n", r.Item, r.Name)
		return nil
	}
	op := ex.field(2)
	defer func() {
		_, _, _ = c.call(ctx, "CloseOperation", handle(op))
	}()
	if r.After == "GetQueryId" {
		// Impala raises an exception for this call, which the recording keeps.
		if _, _, err := c.call(ctx, "GetQueryId", handle(op)); err != nil {
			fmt.Printf("item %d, %s: GetQueryId: %v\n", r.Item, r.Name, err)
		}
	}
	// CancelOperation goes while the statement runs, and while a fetch waits
	// for its first row, so it needs a request of its own.
	var wg sync.WaitGroup
	defer wg.Wait()
	if r.Cancel != "" {
		d, err := time.ParseDuration(r.Cancel)
		if err != nil {
			return fmt.Errorf("parsing the cancel: %w", err)
		}
		wg.Go(func() {
			time.Sleep(d)
			if _, _, err := c.call(ctx, "CancelOperation", handle(op)); err != nil {
				fmt.Printf("item %d, %s: CancelOperation: %v\n", r.Item, r.Name, err)
			}
		})
	}
	for range 3000 {
		st, _, err := c.call(ctx, "GetOperationStatus", handle(op))
		if err != nil {
			return err
		}
		state := st.field(2)
		if state != nil && slices.Contains([]int64{opFinished, opCanceled, opClosed, opError, opTimedOut}, state.i) {
			if state.i != opFinished {
				fmt.Printf("item %d, %s: the operation ended in state %d\n", r.Item, r.Name, state.i)
				return nil
			}
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, _, err := c.call(ctx, "GetResultSetMetadata", handle(op)); err != nil {
		return err
	}
	rows := int64(r.Rows)
	if rows == 0 {
		rows = 1000
	}
	if r.After == "GetLog" {
		if _, _, err := c.call(ctx, "FetchResults", func(e *dbimp.ThriftEncoder) {
			handle(op)(e)
			e.Field(dbimp.ThriftI32, 2)
			e.I32(0)
			e.Field(dbimp.ThriftI64, 3)
			e.I64(100)
			e.Field(dbimp.ThriftI16, 4)
			e.I16(1)
		}); err != nil {
			return err
		}
	}
	for page := range 10000 {
		fr, _, err := c.call(ctx, "FetchResults", func(e *dbimp.ThriftEncoder) {
			handle(op)(e)
			e.Field(dbimp.ThriftI32, 2)
			e.I32(0)
			e.Field(dbimp.ThriftI64, 3)
			e.I64(rows)
			e.Field(dbimp.ThriftI16, 4)
			e.I16(0)
		})
		if err != nil {
			return err
		}
		if failed(fr) {
			fmt.Printf("item %d, %s: FetchResults failed on page %d\n", r.Item, r.Name, page)
			return nil
		}
		if empty(fr) {
			break
		}
	}
	fmt.Printf("item %d, %s: ok\n", r.Item, r.Name)
	return nil
}

// empty reports whether a FetchResults reply holds no value.
func empty(fr *node) bool {
	cols := fr.field(3).field(3)
	if cols == nil {
		return true
	}
	for _, col := range cols.elems {
		for _, u := range col.fields {
			if l := u.v.field(1); l != nil && len(l.elems) > 0 {
				return false
			}
		}
	}
	return true
}

// runBadCall sends a call with a handle that the server never issued.
func runBadCall(ctx context.Context, c *thriftClient, r Request) error {
	bad := &node{t: dbimp.ThriftStruct, fields: []nodeField{{1, &node{t: dbimp.ThriftStruct, fields: []nodeField{
		{1, &node{t: dbimp.ThriftString, s: bytes.Repeat([]byte{1}, 16)}},
		{2, &node{t: dbimp.ThriftString, s: bytes.Repeat([]byte{2}, 16)}},
	}}}, {2, &node{t: dbimp.ThriftI32}}, {3, &node{t: dbimp.ThriftBool}}}}
	res, _, err := c.call(ctx, r.Call, func(e *dbimp.ThriftEncoder) {
		handle(bad)(e)
		if r.Call == "FetchResults" {
			e.Field(dbimp.ThriftI32, 2)
			e.I32(0)
			e.Field(dbimp.ThriftI64, 3)
			e.I64(10)
		}
	})
	switch {
	case err != nil && !errors.Is(err, dbimp.ErrInvalidValue):
		fmt.Printf("item %d, %s: %s: %v\n", r.Item, r.Name, r.Call, strings.TrimSpace(err.Error()))
		return nil
	case err != nil:
		return err
	}
	_ = res
	fmt.Printf("item %d, %s: %s answered\n", r.Item, r.Name, r.Call)
	return nil
}

// catalog sends a catalog call and reads the operation that it starts.
func catalog(ctx context.Context, c *thriftClient, session *node, r Request) error {
	if r.Catalog == "GetInfo" {
		for _, a := range r.Args {
			v, err := strconv.Atoi(a)
			if err != nil {
				return fmt.Errorf("reading the info type %q: %w", a, err)
			}
			res, _, err := c.call(ctx, "GetInfo", func(e *dbimp.ThriftEncoder) {
				handle(session)(e)
				e.Field(dbimp.ThriftI32, 2)
				e.I32(int32(v)) //nolint:gosec // G115: a small number of the script.
			})
			if err != nil {
				return err
			}
			fmt.Printf("item %d, %s: GetInfo %d failed=%t\n", r.Item, r.Name, v, failed(res))
		}
		return nil
	}
	res, _, err := c.call(ctx, r.Catalog, func(e *dbimp.ThriftEncoder) {
		handle(session)(e)
		for i, a := range r.Args {
			if r.Catalog == "GetTables" && i == 3 {
				e.Field(dbimp.ThriftList, 5)
				e.List(dbimp.ThriftString, 1)
				e.String(a)
				continue
			}
			if a == "" {
				continue
			}
			e.Field(dbimp.ThriftString, int16(i+2))
			e.String(a)
		}
	})
	if err != nil {
		return err
	}
	if failed(res) {
		fmt.Printf("item %d, %s: %s was refused\n", r.Item, r.Name, r.Catalog)
		return nil
	}
	op := res.field(2)
	defer func() {
		_, _, _ = c.call(ctx, "CloseOperation", handle(op))
	}()
	if _, _, err := c.call(ctx, "GetResultSetMetadata", handle(op)); err != nil {
		return err
	}
	for range 100 {
		fr, _, err := c.call(ctx, "FetchResults", func(e *dbimp.ThriftEncoder) {
			handle(op)(e)
			e.Field(dbimp.ThriftI32, 2)
			e.I32(0)
			e.Field(dbimp.ThriftI64, 3)
			e.I64(1000)
			e.Field(dbimp.ThriftI16, 4)
			e.I16(0)
		})
		if err != nil {
			return err
		}
		if failed(fr) || empty(fr) {
			break
		}
	}
	fmt.Printf("item %d, %s: %s ok\n", r.Item, r.Name, r.Catalog)
	return nil
}
