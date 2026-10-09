package snowflake //nolint:testpackage // The tests read the claims of a token and move the clock of a connector, which only the package can do.

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// verify checks the signature of a token with the public key of key, and
// returns its header and its claims.
func verify(t *testing.T, key *rsa.PrivateKey, token string) (string, claims) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the token has %d parts, want 3", len(parts))
	}
	enc := base64.RawURLEncoding
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("reading the signature: %v", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("the signature does not verify with the public key: %v", err)
	}
	head, err := enc.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	body, err := enc.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("reading the claims: %v", err)
	}
	var c claims
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("decoding the claims: %v", err)
	}
	return string(head), c
}

// fingerprint is the text that Snowflake names: the standard base64 of the
// SHA-256 of the PKIX DER bytes of the public key.
func fingerprint(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return "SHA256:" + base64.StdEncoding.EncodeToString(sum[:])
}

// TestTokenIsSigned holds D183: the header is RS256 and JWT, the claims are
// the issuer with the account, the user and the fingerprint of the public key,
// the subject, the time of issue and an expiry 3300 seconds later, and the
// signature verifies with the public key.
func TestTokenIsSigned(t *testing.T) {
	t.Parallel()
	key, text := testKey()
	parsed, err := parseKey(text)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(key) {
		t.Fatal("parseKey read another key than the one that the test wrote")
	}
	cfg := config()
	s, err := newSigner(parsed, cfg.account(), cfg.User)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 123456789, time.UTC)
	token, expires, err := s.sign(now)
	if err != nil {
		t.Fatal(err)
	}
	head, c := verify(t, key, token)
	if head != `{"alg":"RS256","typ":"JWT"}` {
		t.Errorf("the header is %s", head)
	}
	want := claims{
		Issuer:   "ORG-ACCT.ALICE." + fingerprint(t, key),
		Subject:  "ORG-ACCT.ALICE",
		IssuedAt: now.Unix(),
		Expires:  now.Unix() + 3300,
	}
	if c != want {
		t.Errorf("the claims are %+v, want %+v", c, want)
	}
	if !expires.Equal(time.Unix(want.Expires, 0)) {
		t.Errorf("the expiry is %v, want %v", expires, time.Unix(want.Expires, 0))
	}
}

// TestAccountOfTheHost holds that the account of the claims is the host
// without its suffix, cut at the first dot, in upper case. A host with no
// suffix stays as it is.
func TestAccountOfTheHost(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"org-acct.snowflakecomputing.com":                    "ORG-ACCT",
		"Org-Acct.SnowflakeComputing.COM":                    "ORG-ACCT",
		"localhost":                                          "LOCALHOST",
		"127.0.0.1":                                          "127.0.0.1",
		"snowflakecomputing.com":                             "SNOWFLAKECOMPUTING.COM",
		"org-acct.privatelink.snowflakecomputing.com":        "ORG-ACCT",
		"xy12345.us-east-1.snowflakecomputing.com":           "XY12345",
		"xy12345.us-east-1.aws.snowflakecomputing.com":       "XY12345",
		"myorg-myaccount.privatelink.snowflakecomputing.com": "MYORG-MYACCOUNT",
		"myorg-myaccount.snowflakecomputing.com":             "MYORG-MYACCOUNT",
		"127.0.0.1:8443":                                     "127.0.0.1:8443",
	} {
		if got := (&Config{Host: host}).account(); got != want {
			t.Errorf("the account of %q is %q, want %q", host, got, want)
		}
	}
}

// TestTokenRenewal holds D183: the connector signs a new token when less than
// five minutes of the old one are left, and keeps the old one before that.
func TestTokenRenewal(t *testing.T) {
	t.Parallel()
	key, _ := testKey()
	c := NewConnector(config())
	t.Cleanup(func() { _ = c.Close() })
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	first, err := c.bearer()
	if err != nil {
		t.Fatal(err)
	}
	_, claim := verify(t, key, first)
	if claim.IssuedAt != now.Unix() {
		t.Errorf("the token was issued at %d, want %d", claim.IssuedAt, now.Unix())
	}
	// The token lives 3300 seconds, and the renewal starts 300 seconds before
	// its end, so the token is new from 3000 seconds on.
	start := now
	for _, tt := range []struct {
		after time.Duration
		same  bool
	}{
		{0, true},
		{time.Minute, true},
		{2999 * time.Second, true},
		{3000 * time.Second, false},
	} {
		now = start.Add(tt.after)
		got, err := c.bearer()
		if err != nil {
			t.Fatal(err)
		}
		if (got == first) != tt.same {
			t.Fatalf("at +%v the token is the old one: %v, want %v", tt.after, got == first, tt.same)
		}
		if !tt.same {
			if _, claim := verify(t, key, got); claim.IssuedAt != now.Unix() || claim.Expires != now.Unix()+3300 {
				t.Errorf("the new token has the claims %+v, want a token issued at %d", claim, now.Unix())
			}
			first = got
		}
	}
	renewed := first
	again, err := c.bearer()
	if err != nil || again != renewed {
		t.Errorf("a second call signed another token: %v", err)
	}
}

// recorder is a fake server that keeps the headers of each request, and
// answers with the status and the body that it holds.
type recorder struct {
	mu      sync.Mutex
	headers []http.Header
	status  int
	body    string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.headers = append(r.headers, req.Header.Clone())
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, _ = w.Write([]byte(r.body))
}

// TestRequestHeaders holds D183: each request carries the token as a Bearer
// header, the token type, and the content type and accept of JSON, and a token
// that verifies with the public key.
func TestRequestHeaders(t *testing.T) {
	t.Parallel()
	key, _ := testKey()
	rec := &recorder{status: http.StatusOK, body: `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"A","type":"fixed","precision":1,"scale":0,"nullable":false}]},"data":[["1"]]}`}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, false)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.headers) != 1 {
		t.Fatalf("the server got %d requests, want 1", len(rec.headers))
	}
	h := rec.headers[0]
	token, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer ")
	if !ok {
		t.Fatalf("the header Authorization is %q, want a Bearer token", h.Get("Authorization")[:min(len(h.Get("Authorization")), 12)])
	}
	if _, c := verify(t, key, token); c.Subject != "ORG-ACCT.ALICE" {
		t.Errorf("the subject is %q", c.Subject)
	}
	for name, want := range map[string]string{ //nolint:gosec // The names are header names.
		"X-Snowflake-Authorization-Token-Type": "KEYPAIR_JWT",
		"Content-Type":                         "application/json",
		"Accept":                               "application/json",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("the header %s is %q, want %q", name, got, want)
		}
	}
}

// TestKeyStaysOut holds D94: no error, and no value that the driver prints,
// holds the private key or a token. The fake server refuses the token, and a
// connector with a key that is not valid fails to connect.
func TestKeyStaysOut(t *testing.T) {
	t.Parallel()
	key, text := testKey()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{text, text[:60], base64.StdEncoding.EncodeToString(der)[:60]}
	rec := &recorder{status: http.StatusUnauthorized, body: `{"code":"390144","message":"JWT token is invalid. null"}`}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, false)
	_, err = db.ExecContext(t.Context(), "SELECT 1")
	if err == nil {
		t.Fatal("the server refused the token and the statement gave no error")
	}
	rec.mu.Lock()
	token, _ := strings.CutPrefix(rec.headers[0].Get("Authorization"), "Bearer ")
	rec.mu.Unlock()
	secrets = append(secrets, token, strings.Split(token, ".")[2])
	var e *Error
	if !errors.As(err, &e) || e.HTTPStatus != http.StatusUnauthorized || e.Code != "390144" {
		t.Errorf("the error is %v, want an *Error with HTTP 401 and the code 390144", err)
	}
	bad := config()
	bad.Password = text[:len(text)-5]
	_, badErr := NewConnector(bad).Connect(t.Context())
	if badErr == nil {
		t.Fatal("a connector with a broken key connected")
	}
	for _, e := range []error{err, badErr} {
		for _, secret := range secrets {
			if strings.Contains(e.Error(), secret) {
				t.Errorf("the error %q holds a secret", e)
			}
		}
	}
	if !errors.Is(badErr, dbimp.ErrInvalidValue) {
		t.Errorf("the error of a broken key is %v, want one that wraps dbimp.ErrInvalidValue", badErr)
	}
}
