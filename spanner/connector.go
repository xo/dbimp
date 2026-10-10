package spanner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels an operation, and the request
// that rolls back a transaction, after the context ended, as D67 bounds the
// rollback of Neo4j and D133 that of Pinot (D191).
const stopTimeout = 5 * time.Second

// maxSmallAnswer is the most of an answer that the driver reads when the
// answer is one small object: a session, a transaction, a commit and an
// operation. The metadata of an operation holds its statements, so the limit
// is large.
const maxSmallAnswer = 1 << 20

// The interval of the poll of a long running operation. It starts short, so
// that a DDL statement that ends soon waits little, and it doubles up to a
// limit, so that a long statement costs few requests. Recorded operations took
// 3 seconds, and a search index 29 (docs/SPANNER.md). The values are not
// measured.
const (
	pollMin = 100 * time.Millisecond
	pollMax = 2 * time.Second
)

// dialectGoogle is the dialect that the driver covers (D191 item 10). The
// emulator can leave the member out.
const dialectGoogle = "GOOGLE_STANDARD_SQL"

// Connector opens connections to one instance. It owns its transport, and
// every connection shares it, with the token and the sessions. A caller can
// build one from a Config and open it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// The access token, and the signer of the key file. tokGate guards them,
	// and the first request that needs a token reads the key file.
	tokGate gate
	signer  *signer
	token   string
	expires time.Time
	// now returns the time of a token, and the time package by default. The
	// tests set it.
	now func() time.Time

	// sessions maps a database and a role to the multiplexed session of the
	// pair, and sessGate guards it (D191 item 3 and D198).
	sessGate gate
	sessions map[target]string

	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for the interval of a poll, and is sleep by default. The tests
	// set it.
	wait func(ctx context.Context, d time.Duration) error
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default. It reads no key
// file and sends no request, so a key file that is not valid fails the first
// Connect.
func NewConnector(cfg Config) *Connector {
	if cfg.Host == "" {
		cfg.Host = defaultHost
	}
	if cfg.Port == 0 {
		cfg.Port = cfg.defaultPort()
	}
	t := dbimp.NewTransport(&tls.Config{MinVersion: tls.VersionTLS12})
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the token goes to the host of the
		// DSN only (D191 item 11).
		client:   dbimp.NewClient(t, false),
		tokGate:  newGate(),
		sessGate: newGate(),
		sessions: map[target]string{},
		pollMin:  pollMin,
		pollMax:  pollMax,
		wait:     sleep,
	}
}

// Connect satisfies driver.Connector. The first call makes the multiplexed
// session of the database, and checks that the database speaks GoogleSQL. A
// later call sends no request while the session lives.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if _, err := c.session(ctx, c.cfg.target()); err != nil {
		return nil, err
	}
	return &conn{c: c}, nil
}

// Driver satisfies driver.Connector.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the idle connections of the transport. A multiplexed session
// cannot be deleted (recorded: "deleteSession of the multiplexed session"), so
// the server ends it.
func (c *Connector) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}

// escape returns name, a path of names that a Spanner resource holds, with
// each part escaped for a URL.
func escape(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// resource returns the path of a resource, with a verb after it when verb is
// not empty, such as /v1/{session}:executeStreamingSql.
func resource(name, verb string) string {
	p := "/v1/" + escape(name)
	if verb != "" {
		p += ":" + verb
	}
	return p
}

// do sends a request to the server of the DSN, with the token and the headers
// of the API, and returns the response with any status (D191 item 11).
func (c *Connector) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return dbimp.Send(c.client, req)
}

// call sends a request whose answer is one small object, and reads it into
// out. It reads nothing when out is nil. A status that is not 2xx is an *Error.
func (c *Connector) call(ctx context.Context, method, path string, body []byte, out any) error {
	res, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := checkStatus(res); err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxSmallAnswer)); err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		return nil
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxSmallAnswer), out); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	return nil
}

// target is the database of a statement, and the database role that it runs
// as. A session belongs to one target, because the role is fixed when the
// session is made (D198).
type target struct {
	database string
	role     string
}

// target returns the database and the role of the DSN.
func (cfg *Config) target() target {
	return target{database: cfg.Database, role: cfg.DatabaseRole}
}

// creatorRoleMember is the member of the Session resource that names the
// database role of the session. It is the JSON name of creator_role in the
// REST reference of Google. The recordings hold no session with a role, so the
// member is not measured here (D198).
const creatorRoleMember = "creatorRole"

// sessionBody returns the body of the request that makes a multiplexed session
// that runs as role. An empty role leaves the member out.
func sessionBody(role string) ([]byte, error) {
	sess := map[string]any{"multiplexed": true}
	if role != "" {
		sess[creatorRoleMember] = role
	}
	b, err := json.Marshal(map[string]any{"session": sess})
	if err != nil {
		return nil, fmt.Errorf("writing the session request: %w", err)
	}
	return b, nil
}

// session returns the name of the multiplexed session of the target. It makes
// the session when the connector has none, after it checks that the database
// speaks GoogleSQL (D191 items 3 and 10).
func (c *Connector) session(ctx context.Context, tg target) (string, error) {
	database := tg.database
	if database == "" {
		return "", fmt.Errorf("opening a session: the DSN names no database: %w", dbimp.ErrInvalidValue)
	}
	if err := c.sessGate.lock(ctx); err != nil {
		return "", err
	}
	defer c.sessGate.unlock()
	if name := c.sessions[tg]; name != "" {
		return name, nil
	}
	db := c.cfg.databaseName(database)
	var info struct {
		Dialect string `json:"databaseDialect"`
	}
	if err := c.call(ctx, http.MethodGet, resource(db, ""), nil, &info); err != nil {
		return "", fmt.Errorf("reading the database %s: %w", database, err)
	}
	if info.Dialect != "" && info.Dialect != dialectGoogle {
		return "", fmt.Errorf("opening the database %s: the dialect is %s, and the driver covers %s only: %w", database, info.Dialect, dialectGoogle, dbimp.ErrNotSupported)
	}
	var sess struct {
		Name string `json:"name"`
	}
	body, err := sessionBody(tg.role)
	if err != nil {
		return "", err
	}
	if err := c.call(ctx, http.MethodPost, resource(db+"/sessions", ""), body, &sess); err != nil {
		return "", fmt.Errorf("making a session in the database %s: %w", database, err)
	}
	if sess.Name == "" {
		return "", fmt.Errorf("making a session in the database %s: the answer has no name: %w", database, dbimp.ErrInvalidValue)
	}
	c.sessions[tg] = sess.Name
	return sess.Name, nil
}

// forget drops the session of the target when it is still name, so that the
// next statement makes a new one.
func (c *Connector) forget(tg target, name string) {
	// The call has no context, and the lock is held for the time of a map
	// access, so it waits for the lock.
	c.sessGate.hold()
	defer c.sessGate.unlock()
	if c.sessions[tg] == name {
		delete(c.sessions, tg)
	}
}

// stream sends a statement to executeStreamingSql in the session of the
// database, and returns the response when its status is 200. Any other
// response is an error. A statement that the server answers with NOT_FOUND for
// its session did not run, so the connector drops the session and the error
// wraps driver.ErrBadConn, and database/sql sends the statement again on a
// new session (D8 and D191 item 3).
func (c *Connector) stream(ctx context.Context, tg target, body []byte) (*http.Response, error) {
	name, err := c.session(ctx, tg)
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, http.MethodPost, resource(name, "executeStreamingSql"), body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, c.lost(tg, name, err)
	}
	return res, nil
}

// post sends a request to a verb of the session of the database, and reads one
// small object from the answer into out.
func (c *Connector) post(ctx context.Context, tg target, verb string, body []byte, out any) error {
	name, err := c.session(ctx, tg)
	if err != nil {
		return err
	}
	return c.lost(tg, name, c.call(ctx, http.MethodPost, resource(name, verb), body, out))
}

// lost turns the error of a session that the server does not know into one
// that wraps driver.ErrBadConn, and drops the session. It returns any other
// error as it is.
func (c *Connector) lost(tg target, name string, err error) error {
	if err == nil || !errors.Is(err, ErrSessionNotFound) {
		return err
	}
	c.forget(tg, name)
	return fmt.Errorf("the session %s is gone: %w: %w", name, driver.ErrBadConn, err)
}

// sleep waits for d, or until ctx ends, and returns the error of ctx then.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-t.C:
		return nil
	}
}

// operation is the answer of a long running operation.
type operation struct {
	Name  string     `json:"name"`
	Done  bool       `json:"done"`
	Error *wireError `json:"error"`
}

// ddl runs DDL statements with one updateDatabaseDdl request, and polls the one
// operation until it is done (D191 item 8 and D198). An error of the operation
// is the error of the call. When ctx ends first, it cancels the operation on the server and
// returns the error of ctx (D191 item 4). The driver names the operation, so
// that it can cancel it even when the answer of the first request did not come.
func (c *Connector) ddl(ctx context.Context, database string, statements []string) error {
	db := c.cfg.databaseName(database)
	id := "dbimp_" + strings.ToLower(rand.Text())
	name := db + "/operations/" + id
	body, err := json.Marshal(struct {
		Statements  []string `json:"statements"`
		OperationID string   `json:"operationId"`
	}{statements, id})
	if err != nil {
		return fmt.Errorf("writing the DDL request: %w", err)
	}
	var op operation
	if err := c.call(ctx, http.MethodPatch, resource(db+"/ddl", ""), body, &op); err != nil {
		if ctx.Err() != nil {
			return c.cancelOperation(ctx, name, err)
		}
		return err
	}
	if op.Name != "" {
		name = op.Name
	}
	delay := c.pollMin
	for !op.Done {
		if err := c.wait(ctx, delay); err != nil {
			return c.cancelOperation(ctx, name, fmt.Errorf("waiting for the operation %s: %w", name, err))
		}
		op = operation{}
		if err := c.call(ctx, http.MethodGet, resource(name, ""), nil, &op); err != nil {
			if ctx.Err() != nil {
				return c.cancelOperation(ctx, name, err)
			}
			return fmt.Errorf("reading the operation %s: %w", name, err)
		}
		delay = min(delay*2, c.pollMax)
	}
	if op.Error != nil {
		return newErr(*op.Error)
	}
	return nil
}

// cancelOperation sends POST {operation}:cancel with ctx without its end and
// the limit of a stop, because ctx has ended. It returns cause, joined with the
// error of the cancel when the cancel fails. A cancel of an operation that does
// not exist answers HTTP 404 (recorded: "cancel an operation that does not
// exist"), and a cancel of a finished one answers HTTP 200 and changes nothing.
func (c *Connector) cancelOperation(ctx context.Context, name string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	err := c.call(ctx, http.MethodPost, resource(name, "cancel"), []byte("{}"), nil)
	if serr, ok := errors.AsType[*Error](err); ok && serr.HTTPStatus == http.StatusNotFound {
		// The server never got the statement, so there is nothing to cancel.
		return cause
	}
	if err != nil {
		return errors.Join(cause, fmt.Errorf("canceling the operation %s: %w", name, err))
	}
	return cause
}
