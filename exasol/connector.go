package exasol

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strconv"

	"github.com/xo/dbimp"
)

// driverName is the name that the login sends as the driver.
const driverName = "dbimp"

// Connector opens connections to one server. It holds no connection itself,
// because each connection owns its WebSocket. A caller can build one from a
// Config and open it with sql.OpenDB.
type Connector struct {
	cfg Config
	tls *tls.Config
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg,
// and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	cfg = cfg.withDefaults()
	return &Connector{cfg: cfg, tls: cfg.tlsConfig()}
}

// tlsConfig returns the TLS configuration for the connection. A fingerprint
// takes the place of the check of the chain, and it holds even when the key
// validateservercertificate is false (D205).
func (cfg *Config) tlsConfig() *tls.Config {
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.SkipVerify || cfg.CertificateFingerprint != "" {
		tc.InsecureSkipVerify = true
	}
	if want, err := hex.DecodeString(cfg.CertificateFingerprint); err == nil && len(want) == sha256.Size {
		// VerifyConnection runs for a resumed session too, where
		// VerifyPeerCertificate does not.
		tc.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("checking the certificate: the server sent none: %w", dbimp.ErrInvalidValue)
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				return fmt.Errorf("checking the certificate: its SHA-256 is %x, not the fingerprint of the DSN: %w", sum, dbimp.ErrInvalidValue)
			}
			return nil
		}
	}
	return tc
}

// url returns the address of the WebSocket.
func (cfg *Config) url() string {
	scheme := "wss"
	if cfg.NoTLS {
		scheme = "ws"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/"
}

// Connect satisfies driver.Connector. It opens a WebSocket and logs in. The
// credentials go only to the host that the DSN names, because the client
// follows no redirect (D212).
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	ws, err := dbimp.DialWebSocket(ctx, c.cfg.url(), dbimp.WithWebSocketTLS(c.tls))
	if err != nil {
		return nil, fmt.Errorf("connecting to the server: %w", err)
	}
	s := &session{ws: ws, autocommit: !c.cfg.NoAutocommit, schema: c.cfg.Schema}
	if err := c.login(ctx, s); err != nil {
		s.fail(err)
		return nil, err
	}
	return &conn{c: c, s: s}, nil
}

// Driver satisfies driver.Connector.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// loginRequest is the second message of a login. It has no command, and it
// holds the password under the public key of the session (recorded: "login
// with protocol 3: send the credentials").
type loginRequest struct {
	Username       string         `json:"username"`
	Password       string         `json:"password"`
	UseCompression bool           `json:"useCompression"`
	ClientName     string         `json:"clientName"`
	DriverName     string         `json:"driverName"`
	Attributes     map[string]any `json:"attributes"`
}

// login runs the two messages of a login. The first asks for the public key,
// and the second sends the credentials. Neither is compressed (recorded:
// "send the credentials and ask for compression").
func (c *Connector) login(ctx context.Context, s *session) error {
	env, err := s.call(ctx, struct {
		Command         string `json:"command"`
		ProtocolVersion int    `json:"protocolVersion"`
	}{cmdLogin, c.cfg.Protocol})
	if err != nil {
		return fmt.Errorf("logging in: asking for the key: %w", err)
	}
	enc, err := encryptPassword(env.ResponseData.PublicKeyModulus, env.ResponseData.PublicKeyExponent, c.cfg.Password)
	if err != nil {
		return fmt.Errorf("logging in: %w", err)
	}
	attrs := map[string]any{"autocommit": !c.cfg.NoAutocommit}
	if !c.cfg.NoCompression {
		attrs["compressionEnabled"] = true
	}
	if c.cfg.Schema != "" {
		attrs["currentSchema"] = c.cfg.Schema
	}
	if c.cfg.QueryTimeout > 0 {
		attrs["queryTimeout"] = c.cfg.QueryTimeout
	}
	env, err = s.call(ctx, loginRequest{
		Username:       c.cfg.User,
		Password:       enc,
		UseCompression: !c.cfg.NoCompression,
		ClientName:     c.cfg.ClientName,
		DriverName:     driverName,
		Attributes:     attrs,
	})
	if err != nil {
		return fmt.Errorf("logging in: sending the credentials: %w", err)
	}
	s.stateMu.Lock()
	s.tz = env.ResponseData.TimeZone
	s.maxData = env.ResponseData.MaxDataMessageSize
	s.stateMu.Unlock()
	s.compress = !c.cfg.NoCompression
	return nil
}

// encryptPassword returns the password under the public key that the server
// sent, with the padding of PKCS #1 version 1.5, in Base64. The key is the
// modulus and the exponent in hex.
func encryptPassword(modulus, exponent, password string) (string, error) {
	n, ok := new(big.Int).SetString(modulus, 16)
	if !ok {
		return "", fmt.Errorf("reading the public key: the modulus is not hex: %w", dbimp.ErrInvalidValue)
	}
	e, ok := new(big.Int).SetString(exponent, 16)
	if !ok || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 {
		return "", fmt.Errorf("reading the public key: the exponent is not a small hex number: %w", dbimp.ErrInvalidValue)
	}
	key := &rsa.PublicKey{N: n, E: int(e.Int64())}
	// The server reads the password with the padding of PKCS #1 version 1.5, and
	// it offers no other (docs/EXASOL.md, Requests).
	b, err := rsa.EncryptPKCS1v15(rand.Reader, key, []byte(password)) //nolint:staticcheck // The WebSocket API of Exasol decrypts with PKCS #1 version 1.5 only.
	if err != nil {
		return "", fmt.Errorf("encrypting the password: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
