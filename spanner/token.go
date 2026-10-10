package spanner

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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The scopes of the token (docs/SPANNER.md, "Requests"): data for the
// statements, and admin for the DDL.
const scopes = "https://www.googleapis.com/auth/spanner.data https://www.googleapis.com/auth/spanner.admin"

// defaultTokenURI is the endpoint that a key file names when it names none.
const defaultTokenURI = "https://oauth2.googleapis.com/token" //nolint:gosec // G101: the address of the token endpoint of Google, which is no secret.

// The lifetime of the JWT that the driver signs, and how long before the end
// of an access token the driver asks for a new one (D191 item 5 and
// docs/SPANNER.md). The endpoint takes at most one hour for a JWT.
const (
	jwtLifetime  = 55 * time.Minute
	tokenRenew   = 5 * time.Minute
	defaultToken = time.Hour
)

// maxTokenAnswer is the most of an answer of the token endpoint that the
// driver reads.
const maxTokenAnswer = 64 << 10

// header is the header of the JWT. A fixed text writes it, so that the
// signature never depends on the order of a map.
const header = `{"alg":"RS256","typ":"JWT"}`

// keyFile is the key file of a service account, with the members that the
// driver reads.
type keyFile struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// signer signs the JWT of one service account, and asks the token endpoint
// for the access token.
type signer struct {
	key      *rsa.PrivateKey
	email    string
	tokenURI string
}

// loadSigner reads the key file at path. No error holds the text of the key
// (D94 and D191 item 5).
func loadSigner(path string) (*signer, error) {
	text, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the key file in the DSN, and the driver reads it as the caller asked.
	if err != nil {
		return nil, fmt.Errorf("reading the credential file: %w", err)
	}
	var kf keyFile
	if err := json.Unmarshal(text, &kf); err != nil {
		return nil, fmt.Errorf("reading the credential file %s: it is not a key file of a service account: %w", path, dbimp.ErrInvalidValue)
	}
	if kf.Type != "" && kf.Type != "service_account" {
		return nil, fmt.Errorf("reading the credential file %s: the type is %q, and the driver reads service_account: %w", path, kf.Type, dbimp.ErrInvalidValue)
	}
	if kf.ClientEmail == "" {
		return nil, fmt.Errorf("reading the credential file %s: it has no client_email: %w", path, dbimp.ErrInvalidValue)
	}
	key, err := parseKey(kf.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("reading the credential file %s: %w", path, err)
	}
	uri := kf.TokenURI
	if uri == "" {
		uri = defaultTokenURI
	}
	if u, err := url.Parse(uri); err != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("reading the credential file %s: token_uri is not an http or https URL: %w", path, dbimp.ErrInvalidValue)
	}
	return &signer{key: key, email: kf.ClientEmail, tokenURI: uri}, nil
}

// parseKey reads the private_key of a key file: the PEM text of the PKCS 8
// bytes of an RSA key. No error holds the text of the key.
func parseKey(text string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("reading the private key: it is not PEM text: %w", dbimp.ErrInvalidValue)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: it is not a PKCS 8 key: %w", dbimp.ErrInvalidValue)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("reading the private key: it is not an RSA key: %w", dbimp.ErrInvalidValue)
	}
	return key, nil
}

// claims are the claims of the JWT (docs/SPANNER.md, "Requests").
type claims struct {
	Issuer   string `json:"iss"`
	Scope    string `json:"scope"`
	Audience string `json:"aud"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// assertion returns the JWT issued at now. It signs with RS256, which is
// RSASSA-PKCS1-v1_5 with SHA-256.
func (s *signer) assertion(now time.Time) (string, error) {
	issued := now.Truncate(time.Second)
	body, err := json.Marshal(claims{
		Issuer:   s.email,
		Scope:    scopes,
		Audience: s.tokenURI,
		IssuedAt: issued.Unix(),
		Expires:  issued.Add(jwtLifetime).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("writing the claims: %w", err)
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("signing the token: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// tokenAnswer is the answer of the token endpoint.
type tokenAnswer struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// exchange sends the JWT to the token endpoint as a form, and returns the
// access token and the time that it expires. The endpoint is the one that the
// key file names, and the request goes to it only (D191 item 5).
func (s *signer) exchange(ctx context.Context, client *http.Client, now time.Time) (string, time.Time, error) {
	jwt, err := s.assertion(now)
	if err != nil {
		return "", time.Time{}, err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("making the token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := dbimp.Send(client, req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("asking %s for a token: %w", s.tokenURI, err)
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return "", time.Time{}, fmt.Errorf("asking %s for a token: %w", s.tokenURI, err)
	}
	defer res.Body.Close()
	var ans tokenAnswer
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxTokenAnswer), &ans); err != nil {
		return "", time.Time{}, fmt.Errorf("reading the answer of %s: %w", s.tokenURI, err)
	}
	if ans.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("reading the answer of %s: no access_token: %w", s.tokenURI, dbimp.ErrInvalidValue)
	}
	life := time.Duration(ans.ExpiresIn) * time.Second
	if life <= 0 {
		life = defaultToken
	}
	return ans.AccessToken, now.Add(life), nil
}

// gate is a lock that a caller can stop waiting for when its context ends.
type gate chan struct{}

// newGate returns an open gate.
func newGate() gate {
	return make(gate, 1)
}

// lock takes the gate, or returns the error of ctx.
func (g gate) lock(ctx context.Context) error {
	select {
	case g <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for a lock: %w", context.Cause(ctx))
	}
}

// hold takes the gate and waits for it as long as it takes. It is for a
// caller that has no context and holds the gate for a short time only.
func (g gate) hold() {
	g <- struct{}{}
}

// unlock opens the gate.
func (g gate) unlock() {
	<-g
}

// bearer returns the access token for a request, or "" when the connector has
// no credential, as for the emulator. It signs a JWT and asks the token
// endpoint when it has no token, and when less than five minutes of the old
// one are left.
func (c *Connector) bearer(ctx context.Context) (string, error) {
	if c.cfg.Token != nil {
		tok, err := c.cfg.Token(ctx)
		if err != nil {
			return "", fmt.Errorf("getting a token: %w", err)
		}
		return tok, nil
	}
	if c.cfg.CredentialFile == "" {
		return "", nil
	}
	if err := c.tokGate.lock(ctx); err != nil {
		return "", err
	}
	defer c.tokGate.unlock()
	now := c.clock()
	if c.token != "" && now.Before(c.expires.Add(-tokenRenew)) {
		return c.token, nil
	}
	if c.signer == nil {
		s, err := loadSigner(c.cfg.CredentialFile)
		if err != nil {
			return "", err
		}
		c.signer = s
	}
	tok, expires, err := c.signer.exchange(ctx, c.client, now)
	if err != nil {
		return "", err
	}
	c.token, c.expires = tok, expires
	return tok, nil
}

// clock returns the time of a token, and the time package by default. The
// tests set now.
func (c *Connector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
