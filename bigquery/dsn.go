package bigquery

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D189).
const (
	keyEndpoint       = "endpoint"
	keyCredentialFile = "credential_file" //nolint:gosec // The text is the name of a key, not a secret.
	keyDisableAuth    = "disable_auth"
	keyScopes         = "scopes"
	keyLocation       = "location"
	keyTimeout        = "timeout"
	keyMaxResults     = "max_results"
)

// defaultEndpoint is the address of the service (recorded: bigquery-034, in
// selfLink).
const defaultEndpoint = "https://bigquery.googleapis.com"

// defaultScope is the scope that the recorded run used (docs/BIGQUERY.md).
const defaultScope = "https://www.googleapis.com/auth/bigquery"

// maxTimeout is the most time that a DSN names for the timeout of a job, which
// is the most milliseconds that the body of a request holds in 32 bits.
const maxTimeout = (1<<31 - 1) * time.Millisecond

// maxMaxResults is the most rows that a DSN names for a page.
const maxMaxResults = 1 << 24

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Project is the id of the project that runs and pays for each statement.
	// It is the host of the DSN, and it is a part of every request path.
	Project string
	// Dataset is the default dataset of each statement, which lets a statement
	// name a table with no dataset. It is optional.
	Dataset string
	// Location is the location of each job, such as US, EU or asia-southeast1.
	// Empty leaves the choice to the service, which uses the location of the
	// dataset of the statement. A job that is not in US or EU needs it on every
	// call that follows, and the driver sends the location that the service
	// answers.
	Location string
	// Endpoint is the address of the service, such as http://127.0.0.1:9050 for
	// an emulator. Empty is https://bigquery.googleapis.com.
	Endpoint string
	// CredentialFile is the path of the key file of a service account, and the
	// secret of the DSN (D189). The driver reads the file when it makes the
	// connector. The key text never sits in a URL.
	CredentialFile string
	// AccessToken is a ready access token, such as the output of gcloud auth
	// print-access-token. The driver sends it as it is and never renews it. A
	// DSN cannot hold it.
	AccessToken string
	// DisableAuth sends no Authorization header, for an emulator, which has no
	// login.
	DisableAuth bool
	// Scopes are the scopes of the token that the driver asks for. Empty is
	// https://www.googleapis.com/auth/bigquery.
	Scopes []string
	// Timeout is the time that the service gives each job, as jobTimeoutMs. Zero
	// sends none.
	Timeout time.Duration
	// MaxResults is the most rows in a page of a result, as maxResults. Zero
	// leaves the page size to the service, which cuts a page at 10 MB.
	MaxResults int
}

// ParseDSN parses a DSN of the form
// bigquery://project/dataset?credential_file=/path/key.json (D27, D35 and
// D189). The host is the project. The path holds the dataset, with an optional
// location before it as /location/dataset, and each is optional. The keys are
// endpoint, credential_file, disable_auth, scopes, location, timeout and
// max_results. The user of the URL is ignored, because dbrun writes one for
// the emulator, and a password is refused, because the secret is a path in
// credential_file. The timeout is a duration with a unit, such as 60s or 1m.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyEndpoint, keyCredentialFile, keyDisableAuth, keyScopes, keyLocation, keyTimeout, keyMaxResults)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Project:        u.Hostname(),
		Location:       q.String(keyLocation, ""),
		CredentialFile: q.String(keyCredentialFile, ""),
	}
	if cfg.Project == "" || strings.Contains(cfg.Project, ":") || u.Port() != "" {
		return nil, fmt.Errorf("parsing the dsn: the host must be the id of a project: %w", dbimp.ErrInvalidValue)
	}
	if _, ok := u.User.Password(); ok {
		return nil, fmt.Errorf("parsing the dsn: the URL has a password, and the secret is the path of a key file in %s: %w", keyCredentialFile, dbimp.ErrInvalidValue)
	}
	pathLocation, dataset, err := parsePath(u)
	if err != nil {
		return nil, err
	}
	cfg.Dataset = dataset
	if pathLocation != "" {
		if q.Has(keyLocation) {
			return nil, fmt.Errorf("parsing the dsn: the location is in the path and in the key %s: %w", keyLocation, dbimp.ErrInvalidValue)
		}
		cfg.Location = pathLocation
	}
	for _, key := range []string{keyEndpoint, keyCredentialFile, keyScopes, keyLocation} {
		if q.Has(key) && q.String(key, "") == "" {
			return nil, fmt.Errorf("parsing key %q: the value is empty: %w", key, dbimp.ErrInvalidValue)
		}
	}
	if q.Has(keyEndpoint) {
		if cfg.Endpoint, err = parseEndpoint(q.String(keyEndpoint, "")); err != nil {
			return nil, err
		}
	}
	if cfg.DisableAuth, err = q.Bool(keyDisableAuth, false); err != nil {
		return nil, err
	}
	if cfg.DisableAuth && cfg.CredentialFile != "" {
		return nil, fmt.Errorf("parsing the dsn: %s and %s cannot both be set: %w", keyDisableAuth, keyCredentialFile, dbimp.ErrInvalidValue)
	}
	if q.Has(keyScopes) {
		if cfg.Scopes, err = parseScopes(q.String(keyScopes, "")); err != nil {
			return nil, err
		}
	}
	if cfg.Timeout, err = q.Duration(keyTimeout, 0); err != nil {
		return nil, err
	}
	if cfg.Timeout < 0 || cfg.Timeout > maxTimeout {
		return nil, fmt.Errorf("parsing key %q: %v: %w", keyTimeout, cfg.Timeout, dbimp.ErrInvalidValue)
	}
	if cfg.MaxResults, err = q.Int(keyMaxResults, 0); err != nil {
		return nil, err
	}
	if cfg.MaxResults < 0 || cfg.MaxResults > maxMaxResults {
		return nil, fmt.Errorf("parsing key %q: %d: %w", keyMaxResults, cfg.MaxResults, dbimp.ErrInvalidValue)
	}
	return cfg, nil
}

// parseEndpoint reads the address of the service: a scheme of http or https, a
// host, and nothing else.
func parseEndpoint(s string) (string, error) {
	u, err := url.Parse(s)
	bad := fmt.Errorf("parsing key %q: the value is not an http or https address with a host: %w", keyEndpoint, dbimp.ErrInvalidValue)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery || u.Path != "" && u.Path != "/" {
		return "", bad
	}
	if h := u.Hostname(); strings.Contains(h, ":") && !strings.HasPrefix(u.Host, "[") {
		return "", bad
	}
	return u.Scheme + "://" + u.Host, nil
}

// parseScopes reads a list of scopes that a space or a comma separates.
func parseScopes(s string) ([]string, error) {
	scopes := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
	if len(scopes) == 0 {
		return nil, fmt.Errorf("parsing key %q: the value has no scope: %w", keyScopes, dbimp.ErrInvalidValue)
	}
	return scopes, nil
}

// parsePath reads the location and the dataset from the path of u. Each is
// optional. One segment is the dataset, and two are the location and the
// dataset.
func parsePath(u *url.URL) (string, string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if p == "" {
		return "", "", nil
	}
	parts := strings.Split(p, "/")
	bad := fmt.Errorf("parsing the dsn: the path %q is not a dataset, or a location and a dataset: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	if len(parts) > 2 {
		return "", "", bad
	}
	names := make([]string, len(parts))
	for i, part := range parts {
		name, err := url.PathUnescape(part)
		if err != nil || name == "" {
			return "", "", bad
		}
		names[i] = name
	}
	if len(names) == 1 {
		return "", names[0], nil
	}
	return names[0], names[1], nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a Config
// that ParseDSN filled. The DSN leaves out AccessToken, because a URL never
// holds a secret.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name, Host: cfg.Project}
	if cfg.Dataset != "" {
		// RawPath keeps a slash in a name escaped, which Path cannot.
		u.Path = "/" + cfg.Dataset
		u.RawPath = "/" + url.PathEscape(cfg.Dataset)
	}
	q := url.Values{}
	if cfg.Endpoint != "" {
		q.Set(keyEndpoint, cfg.Endpoint)
	}
	if cfg.CredentialFile != "" {
		q.Set(keyCredentialFile, cfg.CredentialFile)
	}
	if cfg.DisableAuth {
		q.Set(keyDisableAuth, "true")
	}
	if len(cfg.Scopes) > 0 {
		q.Set(keyScopes, strings.Join(cfg.Scopes, ","))
	}
	if cfg.Location != "" {
		q.Set(keyLocation, cfg.Location)
	}
	if cfg.Timeout > 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
	}
	if cfg.MaxResults > 0 {
		q.Set(keyMaxResults, strconv.Itoa(cfg.MaxResults))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// base returns the address of the service of cfg.
func (cfg *Config) base() string {
	if cfg.Endpoint == "" {
		return defaultEndpoint
	}
	return cfg.Endpoint
}

// scope returns the scopes of the token, as the claims write them.
func (cfg *Config) scope() string {
	if len(cfg.Scopes) == 0 {
		return defaultScope
	}
	return strings.Join(cfg.Scopes, " ")
}
