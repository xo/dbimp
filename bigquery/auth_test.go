package bigquery //nolint:testpackage // The tests set the clock of a connector and read its signer, which only the package can do.

import (
	"crypto"
	"crypto/ed25519"
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

// testEmail is the account of the key file of the tests.
const testEmail = "svc@dbimp-project.iam.gserviceaccount.com"

// keys is the key of the tests. A test never holds a key that came from a server
// or from a person: this one is made here, with crypto/rsa.
var keys = sync.OnceValues(func() (*rsa.PrivateKey, string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("making the key of the tests: " + err.Error())
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic("writing the key of the tests: " + err.Error())
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
})

// keyFileText returns the text of a key file for the token endpoint at tokenURI,
// with the private key text key.
func keyFileText(tb testing.TB, key, tokenURI string) string {
	tb.Helper()
	b, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "dbimp-project",
		"private_key_id": "kid1",
		"private_key":    key,
		"client_email":   testEmail,
		"token_uri":      tokenURI,
	})
	if err != nil {
		tb.Fatal(err)
	}
	return string(b)
}

// writeKeyFile writes a key file in the folder of the test and returns its path.
func writeKeyFile(tb testing.TB, text string) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "key.json")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

// tokenServer is a fake token endpoint. It checks the form that it gets, with
// the public key of the tests, and it answers with a token that names its count.
type tokenServer struct {
	t       *testing.T
	count   atomic.Int32
	expires int
	status  int
	forms   []url.Values
	mu      sync.Mutex
}

func (s *tokenServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(b))
	s.mu.Lock()
	s.forms = append(s.forms, form)
	s.mu.Unlock()
	n := s.count.Add(1)
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		s.t.Errorf("the token request is %s with the type %q", r.Method, r.Header.Get("Content-Type"))
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	expires := s.expires
	if expires == 0 {
		expires = 3600
	}
	b, _ = json.Marshal(map[string]any{"access_token": "tok" + strconv.Itoa(int(n)), "token_type": "Bearer", "expires_in": expires})
	_, _ = w.Write(b)
}

// apiServer is a fake server of the API. It keeps the Authorization header of
// each request, and answers a statement with the recorded answer.
type apiServer struct {
	t    *testing.T
	mu   sync.Mutex
	auth []string
}

func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	s.mu.Lock()
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()
	serve(w, exchange(s.t, 17))
}

// authorizations returns the Authorization header of each request.
func (s *apiServer) authorizations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

// signedDB returns a database whose connector logs in with a key file for the
// token endpoint at tokenURI, and talks to the API server api.
func signedDB(t *testing.T, tokenURI string, api *apiServer) (*sql.DB, *Connector) {
	t.Helper()
	_, key := keys()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	cfg := Config{Project: testProject, CredentialFile: writeKeyFile(t, keyFileText(t, key, tokenURI))}
	c := connector(cfg, srv.URL)
	return openConnector(t, c), c
}

// TestAssertionIsSigned holds D189: the driver signs the header and the claims
// with RS256, with the account, the scope, the token endpoint of the file and a
// lifetime of 55 minutes, and the signature is the one of the public key.
func TestAssertionIsSigned(t *testing.T) {
	t.Parallel()
	priv, key := keys()
	s, err := parseSigner([]byte(keyFileText(t, key, "https://oauth2.example.com/token")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 10, 8, 51, 39, 500000000, time.UTC)
	jwt, err := s.sign(now, defaultScope)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("the assertion has %d parts, want 3", len(parts))
	}
	enc := base64.RawURLEncoding
	headText, err := enc.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var head map[string]string
	if err := json.Unmarshal(headText, &head); err != nil {
		t.Fatal(err)
	}
	if head["alg"] != "RS256" || head["typ"] != "JWT" || head["kid"] != "kid1" || len(head) != 3 {
		t.Errorf("the header is %v, want RS256, JWT and the key id", head)
	}
	claimText, err := enc.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var got claims
	if err := json.Unmarshal(claimText, &got); err != nil {
		t.Fatal(err)
	}
	want := claims{Issuer: testEmail, Scope: defaultScope, Audience: "https://oauth2.example.com/token", IssuedAt: now.Unix(), Expires: now.Unix() + 3300}
	if got != want {
		t.Errorf("the claims are %+v, want %+v", got, want)
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("the signature does not verify with the public key: %v", err)
	}
}

// TestScopesGoInTheClaims holds that the scopes of the DSN are the scope of the
// claims, separated by spaces.
func TestScopesGoInTheClaims(t *testing.T) {
	t.Parallel()
	cfg := Config{Scopes: []string{"https://www.googleapis.com/auth/bigquery", "https://www.googleapis.com/auth/devstorage.read_only"}}
	if got, want := cfg.scope(), "https://www.googleapis.com/auth/bigquery https://www.googleapis.com/auth/devstorage.read_only"; got != want {
		t.Errorf("the scope is %q, want %q", got, want)
	}
	if got := (&Config{}).scope(); got != defaultScope {
		t.Errorf("the default scope is %q, want %q", got, defaultScope)
	}
}

// TestTokenExchange holds D189: the driver posts the signed assertion to the
// token endpoint of the key file, as a form with the grant type of a JWT, and
// sends the access token that it gets as a Bearer token. It keeps the token for
// the next statement.
func TestTokenExchange(t *testing.T) {
	t.Parallel()
	priv, _ := keys()
	tokens := &tokenServer{t: t}
	tsrv := httptest.NewServer(tokens)
	t.Cleanup(tsrv.Close)
	api := &apiServer{t: t}
	db, _ := signedDB(t, tsrv.URL, api)
	for range 2 {
		if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b"); err != nil {
			t.Fatal(err)
		}
	}
	if n := tokens.count.Load(); n != 1 {
		t.Errorf("the driver asked for %d tokens, want 1", n)
	}
	if got := api.authorizations(); len(got) != 2 || got[0] != "Bearer tok1" || got[1] != "Bearer tok1" {
		t.Errorf("the Authorization headers are %v, want Bearer tok1 twice", got)
	}
	form := tokens.forms[0]
	if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(form) != 2 {
		t.Errorf("the form is %v, want the grant type and the assertion", form)
	}
	parts := strings.Split(form.Get("assertion"), ".")
	sig, err := base64.RawURLEncoding.DecodeString(parts[len(parts)-1])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("the assertion does not verify: %v", err)
	}
}

// TestTokenRenewal holds D189: the driver signs a new assertion when less than
// five minutes of the token are left, and not before.
func TestTokenRenewal(t *testing.T) {
	t.Parallel()
	tokens := &tokenServer{t: t}
	tsrv := httptest.NewServer(tokens)
	t.Cleanup(tsrv.Close)
	api := &apiServer{t: t}
	db, c := signedDB(t, tsrv.URL, api)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	for _, tt := range []struct {
		after   time.Duration
		bearer  string
		fetches int32
	}{
		{0, "Bearer tok1", 1},
		{54 * time.Minute, "Bearer tok1", 1},
		{54*time.Minute + 59*time.Second, "Bearer tok1", 1},
		{55 * time.Minute, "Bearer tok2", 2},
		{110 * time.Minute, "Bearer tok3", 3},
	} {
		now = time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC).Add(tt.after)
		if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b"); err != nil {
			t.Fatal(err)
		}
		auth := api.authorizations()
		if got := auth[len(auth)-1]; got != tt.bearer || tokens.count.Load() != tt.fetches {
			t.Errorf("after %v the header is %q after %d fetches, want %q after %d", tt.after, got, tokens.count.Load(), tt.bearer, tt.fetches)
		}
	}
}

// TestShortTokenRenewsEarly holds that a token whose lifetime is shorter than
// ten minutes renews when half of it is left, so that the driver never sends a
// token that ended.
func TestShortTokenRenewsEarly(t *testing.T) {
	t.Parallel()
	tokens := &tokenServer{t: t, expires: 120}
	tsrv := httptest.NewServer(tokens)
	t.Cleanup(tsrv.Close)
	api := &apiServer{t: t}
	db, c := signedDB(t, tsrv.URL, api)
	start := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	now := start
	c.now = func() time.Time { return now }
	for _, after := range []time.Duration{0, 59 * time.Second, 61 * time.Second} {
		now = start.Add(after)
		if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b"); err != nil {
			t.Fatal(err)
		}
	}
	if n := tokens.count.Load(); n != 2 {
		t.Errorf("the driver asked for %d tokens, want 2", n)
	}
}

// TestAccessTokenIsSentAsIs holds D189: a ready access token goes in the header
// as it is, and the driver asks for no other.
func TestAccessTokenIsSentAsIs(t *testing.T) {
	t.Parallel()
	api := &apiServer{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	db := open(t, Config{Project: testProject, AccessToken: "ya29.ready"}, srv.URL)
	if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	if got := api.authorizations(); len(got) != 1 || got[0] != "Bearer ya29.ready" { //nolint:gosec // The text is a token of a test.
		t.Errorf("the Authorization headers are %v, want the token that the caller set", got)
	}
}

// TestDisableAuthSendsNoHeader holds that an emulator, which has no login, gets
// no Authorization header.
func TestDisableAuthSendsNoHeader(t *testing.T) {
	t.Parallel()
	api := &apiServer{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	db := open(t, Config{Project: testProject, DisableAuth: true, AccessToken: "ignored"}, srv.URL)
	if _, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	if got := api.authorizations(); len(got) != 1 || got[0] != "" {
		t.Errorf("the Authorization headers are %v, want none", got)
	}
}

// TestNoCredential holds that a connector with no way to log in fails the
// statement with ErrNoCredential, and sends nothing.
func TestNoCredential(t *testing.T) {
	t.Parallel()
	api := &apiServer{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	db := open(t, Config{Project: testProject}, srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if !errors.Is(err, ErrNoCredential) {
		t.Errorf("the error is %v, want ErrNoCredential", err)
	}
	if got := api.authorizations(); len(got) != 0 {
		t.Errorf("the driver sent %d requests with no credential", len(got))
	}
}

// TestKeyFileErrors holds that a key file that is not valid fails the first
// connection, with an error that holds no text of the key.
func TestKeyFileErrors(t *testing.T) {
	t.Parallel()
	_, key := keys()
	edKey := func() string {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	}()
	for _, tt := range []struct {
		name string
		text string
	}{
		{"not JSON", "not json at all, with the secret-marker"},
		{"wrong type", strings.Replace(keyFileText(t, key, "https://x.example.com/t"), "service_account", "authorized_user", 1)},
		{"no account", strings.Replace(keyFileText(t, key, "https://x.example.com/t"), testEmail, "", 1)},
		{"key is not PEM", keyFileText(t, "secret-marker not pem", "https://x.example.com/t")},
		{"key is not RSA", keyFileText(t, edKey, "https://x.example.com/t")},
		{"key is garbage", keyFileText(t, "-----BEGIN PRIVATE KEY-----\nc2VjcmV0LW1hcmtlcg==\n-----END PRIVATE KEY-----\n", "https://x.example.com/t")},
		{"token endpoint in the clear", keyFileText(t, key, "http://oauth2.example.com/token")},
		{"token endpoint with a login", keyFileText(t, key, "https://u:p@oauth2.example.com/token")},
		{"token endpoint is not an address", keyFileText(t, key, "oauth2")},
	} {
		c := NewConnector(Config{Project: testProject, CredentialFile: writeKeyFile(t, tt.text)})
		_, err := c.Connect(t.Context())
		if err == nil {
			t.Errorf("%s: Connect gave no error", tt.name)
			continue
		}
		if strings.Contains(err.Error(), "secret-marker") || strings.Contains(err.Error(), "c2VjcmV0") || strings.Contains(err.Error(), key[40:80]) {
			t.Errorf("%s: the error holds the text of the key: %v", tt.name, err)
		}
		_ = c.Close()
	}
	c := NewConnector(Config{Project: testProject, CredentialFile: filepath.Join(t.TempDir(), "missing.json")})
	if _, err := c.Connect(t.Context()); err == nil {
		t.Error("a key file that does not exist gave no error")
	}
	_ = c.Close()
}

// TestKeyFileTokenURI holds that a key file with no token endpoint uses the one
// of Google, and that a loopback address in the clear is allowed for the tests.
func TestKeyFileTokenURI(t *testing.T) {
	t.Parallel()
	_, key := keys()
	s, err := parseSigner([]byte(keyFileText(t, key, "")))
	if err != nil || s.tokenURI != defaultTokenURI {
		t.Errorf("the token endpoint is %v, %v, want the default", s, err)
	}
	for _, uri := range []string{"http://127.0.0.1:9/t", "http://localhost/t", "http://[::1]:9/t", "https://oauth2.googleapis.com/token"} {
		if _, err := parseSigner([]byte(keyFileText(t, key, uri))); err != nil {
			t.Errorf("the token endpoint %s gave %v", uri, err)
		}
	}
}

// TestTokenEndpointRefuses holds that a refusal of the token endpoint fails the
// statement before any request of the API, and that the error holds neither the
// assertion nor the key.
func TestTokenEndpointRefuses(t *testing.T) {
	t.Parallel()
	_, key := keys()
	tokens := &tokenServer{t: t, status: http.StatusBadRequest}
	tsrv := httptest.NewServer(tokens)
	t.Cleanup(tsrv.Close)
	api := &apiServer{t: t}
	db, _ := signedDB(t, tsrv.URL, api)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("the error is %v, want the refusal of the token endpoint", err)
	}
	if _, ok := errors.AsType[*dbimp.StatusError](err); !ok {
		t.Errorf("the error %v does not wrap a *dbimp.StatusError", err)
	}
	assertion := tokens.forms[0].Get("assertion")
	if strings.Contains(err.Error(), assertion) || strings.Contains(err.Error(), key[40:80]) {
		t.Errorf("the error holds the assertion or the key: %v", err)
	}
	if got := api.authorizations(); len(got) != 0 {
		t.Errorf("the driver sent %d requests of the API with no token", len(got))
	}
}

// TestTokenEndpointRedirect holds D189 item 10: the driver follows no redirect
// of the token endpoint, so the assertion goes to the endpoint of the key file
// only.
func TestTokenEndpointRedirect(t *testing.T) {
	t.Parallel()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the driver followed a redirect of the token endpoint")
	}))
	t.Cleanup(other.Close)
	tsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(tsrv.Close)
	api := &apiServer{t: t}
	db, _ := signedDB(t, tsrv.URL, api)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err == nil {
		t.Error("a redirect of the token endpoint gave no error")
	}
}

// TestTokenStaysOutOfErrors holds that no error of a statement holds the token.
func TestTokenStaysOutOfErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		serve(w, exchange(t, 420))
	}))
	t.Cleanup(srv.Close)
	db := open(t, Config{Project: testProject, AccessToken: "ya29.very-secret"}, srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if err == nil || strings.Contains(err.Error(), "very-secret") {
		t.Errorf("the error is %v, want one with no token", err)
	}
}
