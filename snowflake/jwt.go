package snowflake

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The lifetime of a token, and how long before its end the driver signs a
// new one (D183 and docs/SNOWFLAKE.md). The server takes at most one hour, and
// dbsetup used 3300 seconds.
const (
	tokenLifetime = 3300 * time.Second
	tokenRenew    = 5 * time.Minute
)

// parseKey reads the private key of a DSN: the base64url text of the PKCS8
// DER bytes of an RSA key, with or without the padding (D183). No error holds
// the text of the key (D94).
func parseKey(text string) (*rsa.PrivateKey, error) {
	der, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(text), "="))
	if err != nil {
		return nil, fmt.Errorf("reading the private key: it is not base64url text: %w", dbimp.ErrInvalidValue)
	}
	// x509 ignores the bytes after the key, and a key with bytes after it is
	// more than one key.
	var outer asn1.RawValue
	if rest, err := asn1.Unmarshal(der, &outer); err != nil || len(rest) > 0 {
		return nil, fmt.Errorf("reading the private key: it is not one PKCS8 key: %w", dbimp.ErrInvalidValue)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("reading the private key: it is not a PKCS8 key: %w", dbimp.ErrInvalidValue)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("reading the private key: it is not an RSA key: %w", dbimp.ErrInvalidValue)
	}
	return key, nil
}

// signer signs the tokens of one user with the private key of the user. It
// holds the name that the claims carry, and the fingerprint of the public
// key.
type signer struct {
	key     *rsa.PrivateKey
	subject string
	issuer  string
}

// newSigner returns the signer of key for the user in the account. The claims
// name the account and the user in upper case, and the account is the form
// <org>-<account> (docs/SNOWFLAKE.md).
func newSigner(key *rsa.PrivateKey, account, user string) (*signer, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("writing the public key: %w", err)
	}
	sum := sha256.Sum256(der)
	subject := strings.ToUpper(account) + "." + strings.ToUpper(user)
	return &signer{
		key:     key,
		subject: subject,
		issuer:  subject + ".SHA256:" + base64.StdEncoding.EncodeToString(sum[:]),
	}, nil
}

// claims are the claims of a token (measured by dbsetup, 2026-10-09).
type claims struct {
	Issuer   string `json:"iss"`
	Subject  string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// header is the header of a token. A fixed text writes it, so that the
// signature never depends on the order of a map.
const header = `{"alg":"RS256","typ":"JWT"}`

// sign returns a token issued at now, and the time that it expires. It signs
// with RS256, which is RSASSA-PKCS1-v1_5 with SHA-256.
func (s *signer) sign(now time.Time) (string, time.Time, error) {
	issued := now.Truncate(time.Second)
	expires := issued.Add(tokenLifetime)
	body, err := json.Marshal(claims{
		Issuer:   s.issuer,
		Subject:  s.subject,
		IssuedAt: issued.Unix(),
		Expires:  expires.Unix(),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("writing the claims: %w", err)
	}
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signing the token: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), expires, nil
}
