package spanner //nolint:testpackage // The tests set the clock of a connector and read its token, which only the package can do.

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// tokenServer is a fake token endpoint. It keeps each form that it got, and
// answers with a token that has the number of the request.
type tokenServer struct {
	srv *httptest.Server

	mu    sync.Mutex
	forms []url.Values
	// status and body override the answer when status is not 0.
	status int
	body   string
	// life is expires_in of the answer.
	life int64
}

func newTokenServer(t *testing.T) *tokenServer {
	t.Helper()
	ts := &tokenServer{life: 3600}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ts.mu.Lock()
		ts.forms = append(ts.forms, r.PostForm)
		n := len(ts.forms)
		status, body, life := ts.status, ts.body, ts.life
		ts.mu.Unlock()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("the token request is %s with the content type %q", r.Method, r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
			return
		}
		out, _ := json.Marshal(map[string]any{"access_token": "token-" + strconv.Itoa(n), "expires_in": life, "token_type": "Bearer"})
		_, _ = w.Write(out)
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *tokenServer) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.forms)
}

// jwtParts splits a JWT and decodes its header and its claims.
func jwtParts(t *testing.T, jwt string) (string, claims, []byte) {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("the JWT has %d parts, want 3", len(parts))
	}
	enc := base64.RawURLEncoding
	head, err := enc.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	body, err := enc.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var c claims
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	return string(head), c, sig
}

// TestAssertionIsSigned holds D191 item 5: the JWT has the header and the claims
// of docs/SPANNER.md, the two scopes, the token endpoint of the key file as its
// audience, a life of 55 minutes, and a signature that the public key checks.
func TestAssertionIsSigned(t *testing.T) {
	t.Parallel()
	ts := newTokenServer(t)
	s, err := loadSigner(writeKeyFile(t, ts.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 10, 5, 46, 32, 123456789, time.UTC)
	jwt, err := s.assertion(now)
	if err != nil {
		t.Fatal(err)
	}
	head, c, sig := jwtParts(t, jwt)
	if head != `{"alg":"RS256","typ":"JWT"}` {
		t.Errorf("the header is %s", head)
	}
	want := claims{
		Issuer:   "test@dbimp-project.iam.gserviceaccount.com",
		Scope:    "https://www.googleapis.com/auth/spanner.data https://www.googleapis.com/auth/spanner.admin",
		Audience: ts.srv.URL,
		IssuedAt: now.Unix(),
		Expires:  now.Unix() + 55*60,
	}
	if c != want {
		t.Errorf("the claims are %+v, want %+v", c, want)
	}
	sum := sha256.Sum256([]byte(jwt[:strings.LastIndex(jwt, ".")]))
	if err := rsa.VerifyPKCS1v15(&keys().PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("the signature does not verify: %v", err)
	}
}

// bearerServer is a fake server for the statements, which keeps the header
// Authorization of each request.
func bearerServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"databaseDialect":"GOOGLE_STANDARD_SQL"}`)
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			_, _ = io.WriteString(w, `{"name":"`+testSession+`"}`)
		default:
			_, _ = io.WriteString(w, streamOf(intField("a"), `"1"`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// TestTokenIsExchangedAndSent holds D191 item 5: the first request signs a JWT,
// sends it to the token endpoint of the key file as a form with the grant type
// of a JWT, and every request then carries the access token as a Bearer token.
func TestTokenIsExchangedAndSent(t *testing.T) {
	t.Parallel()
	ts := newTokenServer(t)
	srv, seen := bearerServer(t)
	cfg := config()
	cfg.CredentialFile = writeKeyFile(t, ts.srv.URL)
	db := open(t, cfg, srv.URL, false)
	for range 2 {
		var a int64
		if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
			t.Fatal(err)
		}
	}
	for i, h := range seen() {
		if h != "Bearer token-1" {
			t.Errorf("request %d carries %q, want Bearer token-1", i, h)
		}
	}
	if n := ts.count(); n != 1 {
		t.Fatalf("the token endpoint got %d requests, want 1", n)
	}
	form := ts.forms[0]
	if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Errorf("the grant type is %q", form.Get("grant_type"))
	}
	if _, c, _ := jwtParts(t, form.Get("assertion")); c.Audience != ts.srv.URL {
		t.Errorf("the audience is %q, want the token endpoint", c.Audience)
	}
}

// TestTokenRenewal holds that the driver keeps the token until five minutes
// before it ends, and then asks for a new one.
func TestTokenRenewal(t *testing.T) {
	t.Parallel()
	ts := newTokenServer(t)
	srv, seen := bearerServer(t)
	cfg := config()
	cfg.CredentialFile = writeKeyFile(t, ts.srv.URL)
	c := connector(cfg, srv.URL, true)
	now := time.Date(2026, time.October, 10, 6, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	query := func() {
		t.Helper()
		var a int64
		if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
			t.Fatal(err)
		}
	}
	query()
	now = now.Add(54 * time.Minute)
	query()
	if n := ts.count(); n != 1 {
		t.Fatalf("the token endpoint got %d requests after 54 minutes, want 1", n)
	}
	now = now.Add(time.Minute + time.Second)
	query()
	if n := ts.count(); n != 2 {
		t.Fatalf("the token endpoint got %d requests after 55 minutes, want 2", n)
	}
	if got, want := seen(), []string{"Bearer token-1", "Bearer token-1", "Bearer token-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the requests carry %q, want %q", got, want)
	}
}

// TestNoCredentialSendsNoHeader holds that a connector with no credential, as the
// emulator needs it, sends no Authorization header.
func TestNoCredentialSendsNoHeader(t *testing.T) {
	t.Parallel()
	srv, seen := bearerServer(t)
	db := open(t, config(), srv.URL, false)
	var a int64
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	for i, h := range seen() {
		if h != "" {
			t.Errorf("request %d carries %q, want no header", i, h)
		}
	}
}

// TestTokenFromTheCaller holds D191 item 5: a caller that passes a ready token
// through Config.Token gets it in every request, and an error of the function is
// the error of the statement.
func TestTokenFromTheCaller(t *testing.T) {
	t.Parallel()
	srv, seen := bearerServer(t)
	cfg := config()
	var fail atomic.Bool
	errToken := errors.New("the token store is down")
	cfg.Token = func(context.Context) (string, error) {
		if fail.Load() {
			return "", errToken
		}
		return "mine", nil
	}
	db := open(t, cfg, srv.URL, false)
	var a int64
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	for i, h := range seen() {
		if h != "Bearer mine" {
			t.Errorf("request %d carries %q, want Bearer mine", i, h)
		}
	}
	fail.Store(true)
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); !errors.Is(err, errToken) {
		t.Errorf("the error is %v, want the error of the function", err)
	}
}

// TestTokenStaysWithTheHost holds D191 item 11: the Bearer token goes to the
// configured host only. The driver follows no redirect, so a server that sends the
// request somewhere else gets an error, and the other host gets nothing.
func TestTokenStaysWithTheHost(t *testing.T) {
	t.Parallel()
	var other atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other.Add(1) }))
	t.Cleanup(elsewhere.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // The test sends the client to a fake server on purpose.
	}))
	t.Cleanup(srv.Close)
	ts := newTokenServer(t)
	cfg := config()
	cfg.CredentialFile = writeKeyFile(t, ts.srv.URL)
	err := open(t, cfg, srv.URL, false).PingContext(t.Context())
	if err == nil {
		t.Fatal("a redirect gave no error")
	}
	if n := other.Load(); n != 0 {
		t.Errorf("the host of the redirect got %d requests, want none", n)
	}
	var serr *Error
	if !errors.As(err, &serr) || serr.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %v, want an *Error with HTTP 307", err)
	}
}

// TestKeyFileErrors holds that a key file that is not valid fails the first
// connection with an error that names the file and the fault, and never holds the
// text of the key (D94 and D191 item 5).
func TestKeyFileErrors(t *testing.T) {
	t.Parallel()
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(keys())})
	write := func(m map[string]string) string {
		text, _ := json.Marshal(m)
		path := filepath.Join(t.TempDir(), "k.json")
		if err := os.WriteFile(path, text, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := map[string]string{ //nolint:gosec // G101: the names of the members of a key file, and a key that the test made.
		"type": "service_account", "client_email": "a@b", "private_key": keyText(), "token_uri": "https://oauth2.googleapis.com/token"}
	with := func(k, v string) map[string]string {
		m := maps.Clone(good)
		m[k] = v
		return m
	}
	notJSON := filepath.Join(t.TempDir(), "k.json")
	if err := os.WriteFile(notJSON, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := keyText()
	for name, tt := range map[string]struct {
		path string
		want error
	}{
		"a missing file":                {filepath.Join(t.TempDir(), "none.json"), os.ErrNotExist},
		"text that is not JSON":         {notJSON, dbimp.ErrInvalidValue},
		"another type":                  {write(with("type", "authorized_user")), dbimp.ErrInvalidValue},
		"no client_email":               {write(with("client_email", "")), dbimp.ErrInvalidValue},
		"no key":                        {write(with("private_key", "")), dbimp.ErrInvalidValue},
		"a key that is not PEM":         {write(with("private_key", "AAAA")), dbimp.ErrInvalidValue},
		"a PKCS 1 key":                  {write(with("private_key", string(pkcs1))), dbimp.ErrInvalidValue},
		"an EC key":                     {write(with("private_key", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER})))), dbimp.ErrInvalidValue},
		"a token_uri that is not a URL": {write(with("token_uri", "not a url")), dbimp.ErrInvalidValue},
		"a token_uri of another scheme": {write(with("token_uri", "ftp://x/token")), dbimp.ErrInvalidValue},
	} {
		_, err := loadSigner(tt.path)
		if err == nil || !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", name, err, tt.want)
			continue
		}
		if strings.Contains(err.Error(), secret[40:80]) || strings.Contains(err.Error(), "AAAA") {
			t.Errorf("%s: the error holds the key: %v", name, err)
		}
	}
	// The first connection reports it, and a connector with no key file sends no
	// request at all before.
	cfg := config()
	cfg.CredentialFile = filepath.Join(t.TempDir(), "none.json")
	srv, seen := bearerServer(t)
	if err := open(t, cfg, srv.URL, false).PingContext(t.Context()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Ping with a missing key file gave %v, want a file error", err)
	}
	if n := len(seen()); n != 0 {
		t.Errorf("the server got %d requests, want none", n)
	}
}

// TestTokenEndpointErrors holds that an error of the token endpoint is the error
// of the first statement, with the status, and that an answer with no token is
// refused.
func TestTokenEndpointErrors(t *testing.T) {
	t.Parallel()
	srv, _ := bearerServer(t)
	for name, tt := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"a refused JWT":  {http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`, nil},
		"no token":       {http.StatusOK, `{"expires_in":3600}`, dbimp.ErrInvalidValue},
		"text, not JSON": {http.StatusOK, `nope`, nil},
	} {
		ts := newTokenServer(t)
		ts.status, ts.body = tt.status, tt.body
		cfg := config()
		cfg.CredentialFile = writeKeyFile(t, ts.srv.URL)
		err := open(t, cfg, srv.URL, false).PingContext(t.Context())
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v", name, err)
			continue
		}
		if got, want := errors.Is(err, dbimp.ErrAuthentication), tt.status == http.StatusBadRequest; got != want {
			t.Errorf("%s: errors.Is(err, dbimp.ErrAuthentication) is %t, want %t", name, got, want)
		}
		if tt.status == http.StatusBadRequest {
			var serr *dbimp.StatusError
			if !errors.As(err, &serr) || serr.Code != tt.status || !strings.Contains(serr.Body, "invalid_grant") {
				t.Errorf("%s: the error is %v, want a *dbimp.StatusError with the answer of the endpoint", name, err)
			}
		}
		if strings.Contains(err.Error(), "assertion") && strings.Contains(err.Error(), "eyJ") {
			t.Errorf("%s: the error holds the JWT: %v", name, err)
		}
	}
}

// TestTokenWaitStops holds that a request that waits for the token of another
// request stops when its context ends.
func TestTokenWaitStops(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = io.WriteString(w, `{"access_token":"t","expires_in":3600}`)
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	cfg := config()
	cfg.CredentialFile = writeKeyFile(t, slow.URL)
	srv, _ := bearerServer(t)
	c := connector(cfg, srv.URL, true)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	first := make(chan error, 1)
	go func() {
		var a int64
		first <- db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a)
	}()
	for deadline := time.Now().Add(10 * time.Second); len(c.tokGate) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the first request never asked for the token")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	var a int64
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&a); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the second request gave %v, want context.DeadlineExceeded", err)
	}
	_ = first
}
