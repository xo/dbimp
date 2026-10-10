package bigquery

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The lifetime of a signed request for a token, and how long before the end of
// an access token the driver asks for a new one (D189 and docs/BIGQUERY.md).
// The service takes at most one hour, and the recorded run used 55 minutes.
const (
	assertionLifetime = 55 * time.Minute
	tokenRenew        = 5 * time.Minute
)

// defaultTokenURI is the token endpoint of Google, for a key file that names
// none.
const defaultTokenURI = "https://oauth2.googleapis.com/token" //nolint:gosec // The text is the address of the token endpoint, not a secret.

// grantType is the grant of a signed assertion (docs/BIGQUERY.md).
const grantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// maxTokenAnswer is the most of the answer of the token endpoint that the
// driver reads. The answer is a small object.
const maxTokenAnswer = 64 << 10

// keyFile is the part of the key file of a service account that the driver
// reads (docs/BIGQUERY.md).
type keyFile struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	TokenURI     string `json:"token_uri"`
}

// signer signs the assertions of one service account with its private key.
type signer struct {
	key      *rsa.PrivateKey
	keyID    string
	email    string
	tokenURI string
}

// readSigner reads the key file at path. No error holds the text of the key.
func readSigner(path string) (*signer, error) {
	data, err := os.ReadFile(path) //nolint:gosec // The caller names the file of its own key.
	if err != nil {
		return nil, fmt.Errorf("reading the key file: %w", err)
	}
	return parseSigner(data)
}

// parseSigner reads the text of a key file.
func parseSigner(data []byte) (*signer, error) {
	var kf keyFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("reading the key file: it is not a JSON object: %w", dbimp.ErrInvalidValue)
	}
	switch {
	case kf.Type != "service_account":
		return nil, fmt.Errorf("reading the key file: the type is %q, and the driver reads service_account: %w", kf.Type, dbimp.ErrInvalidValue)
	case kf.ClientEmail == "":
		return nil, fmt.Errorf("reading the key file: it has no client_email: %w", dbimp.ErrInvalidValue)
	}
	key, err := parseKey(kf.PrivateKey)
	if err != nil {
		return nil, err
	}
	if kf.TokenURI == "" {
		kf.TokenURI = defaultTokenURI
	}
	if err := checkTokenURI(kf.TokenURI); err != nil {
		return nil, err
	}
	return &signer{key: key, keyID: kf.PrivateKeyID, email: kf.ClientEmail, tokenURI: kf.TokenURI}, nil
}

// parseKey reads the private key of a key file: a PEM text of a PKCS8 key, or
// of a PKCS1 key, of RSA. No error holds the text of the key.
func parseKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("reading the private key: it is not PEM text: %w", dbimp.ErrInvalidValue)
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: it is not a PKCS8 key: %w", dbimp.ErrInvalidValue)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("reading the private key: it is not an RSA key: %w", dbimp.ErrInvalidValue)
	}
	return key, nil
}

// checkTokenURI makes sure that the token endpoint is an https address, or an
// http address on the machine of the caller, so that the signed assertion never
// crosses the network in the clear.
func checkTokenURI(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("reading the key file: token_uri is not an address: %w", dbimp.ErrInvalidValue)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if h := u.Hostname(); h == "localhost" || isLoopback(h) {
			return nil
		}
	}
	return fmt.Errorf("reading the key file: token_uri must be an https address: %w", dbimp.ErrInvalidValue)
}

// isLoopback reports whether host is a loopback IP address.
func isLoopback(host string) bool {
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// header is the header of an assertion without a key id. A fixed text writes
// it, so that the signature never depends on the order of a map.
const header = `{"alg":"RS256","typ":"JWT"}`

// claims are the claims of an assertion (docs/BIGQUERY.md).
type claims struct {
	Issuer   string `json:"iss"`
	Scope    string `json:"scope"`
	Audience string `json:"aud"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// sign returns an assertion issued at now for the scope. It signs with RS256,
// which is RSASSA-PKCS1-v1_5 with SHA-256.
func (s *signer) sign(now time.Time, scope string) (string, error) {
	issued := now.Truncate(time.Second)
	body, err := json.Marshal(claims{
		Issuer:   s.email,
		Scope:    scope,
		Audience: s.tokenURI,
		IssuedAt: issued.Unix(),
		Expires:  issued.Add(assertionLifetime).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("writing the claims: %w", err)
	}
	head := header
	if s.keyID != "" {
		kid, err := json.Marshal(s.keyID)
		if err != nil {
			return "", fmt.Errorf("writing the header: %w", err)
		}
		head = `{"alg":"RS256","typ":"JWT","kid":` + string(kid) + `}`
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString([]byte(head)) + "." + enc.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("signing the assertion: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// bearer returns the access token for the Authorization header, and "" when the
// connector sends no token, which is the case for an emulator that has no key
// file, a token or an explicit disable_auth. It exchanges a new assertion when it has no token,
// and when the old one has less than five minutes left (D189). A token that the
// caller set is never renewed.
func (c *Connector) bearer(ctx context.Context) (string, error) {
	switch {
	case c.cfg.DisableAuth:
		return "", nil
	case c.cfg.AccessToken != "":
		return c.cfg.AccessToken, nil
	case c.cfg.Endpoint != "" && c.signer == nil && c.signErr == nil:
		// An endpoint with no key file and no token is an emulator, which has no
		// login (D195 item 7).
		return "", nil
	case c.signErr != nil:
		return "", c.signErr
	case c.signer == nil:
		return "", fmt.Errorf("logging in: set credential_file, an access token, or disable_auth for an emulator: %w", ErrNoCredential)
	}
	now := c.clock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" || !now.Before(c.expires) {
		token, lifetime, err := c.exchange(ctx, now)
		if err != nil {
			return "", err
		}
		c.token = token
		c.expires = now.Add(lifetime - min(tokenRenew, lifetime/2))
	}
	return c.token, nil
}

// clock returns the time of a token: the time package by default, and the
// function that a test sets.
func (c *Connector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// exchange signs an assertion and posts it to the token endpoint of the key
// file. It returns the access token and its lifetime. The request goes through
// the client of the connector, which follows no redirect.
func (c *Connector) exchange(ctx context.Context, now time.Time) (string, time.Duration, error) {
	assertion, err := c.signer.sign(now, c.cfg.scope())
	if err != nil {
		return "", 0, err
	}
	form := url.Values{"grant_type": {grantType}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.signer.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("making the request for a token: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return "", 0, fmt.Errorf("exchanging the key for a token: %w", err)
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return "", 0, fmt.Errorf("exchanging the key for a token: %w", tokenRefused(err))
	}
	defer res.Body.Close()
	var answer struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxTokenAnswer), &answer); err != nil {
		return "", 0, fmt.Errorf("reading the answer of the token endpoint: %w", err)
	}
	if answer.AccessToken == "" {
		return "", 0, fmt.Errorf("reading the answer of the token endpoint: no access_token: %w", dbimp.ErrInvalidValue)
	}
	lifetime := time.Duration(answer.ExpiresIn) * time.Second
	if lifetime <= 0 {
		// The document of Google says 3600 seconds.
		lifetime = time.Hour
	}
	return answer.AccessToken, lifetime, nil
}

// tokenRefused wraps err with dbimp.ErrAuthentication when err is the answer
// of the token endpoint that refuses the key: the error invalid_grant or
// invalid_client in the body (D197). Any other error comes back as it is. An
// answer with HTTP 401 matches already, through the *dbimp.StatusError.
func tokenRefused(err error) error {
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(serr.Body), &body) != nil {
		return err
	}
	if body.Error == "invalid_grant" || body.Error == "invalid_client" {
		return fmt.Errorf("%w: %w", dbimp.ErrAuthentication, err)
	}
	return err
}
