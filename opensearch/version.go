package opensearch

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// maxRelease is the most of the answer to GET / that the driver reads. The
// answer is one small object (measured).
const maxRelease = 64 << 10

// releaseHeader is the header X-OpenSearch-Version in the form that net/http
// gives it. The 3 series sends it with each answer, also
// to a user that GET / refuses. Its value is "OpenSearch/3.9.0 (opensearch)".
// The 2 series sends none (measured).
const releaseHeader = "X-Opensearch-Version"

// version answers SELECT version(), which SQL in OpenSearch has no function
// for (D181). It uses the release that an earlier answer of the connection
// carried in the header X-OpenSearch-Version, and otherwise it sends GET /.
// The answer to GET / holds version.number for the administrator only. On the
// 3 series the header also reaches the ordinary user, and the 2 series gives
// that user HTTP 403, which the driver passes on as it is.
func (c *conn) version(ctx context.Context) (driver.Rows, error) {
	if c.release == "" {
		release, err := c.c.release(ctx)
		if err != nil {
			return nil, err
		}
		c.release = release
	}
	return dbimp.NewVersionRows(c.release), nil
}

// release sends GET /, and returns the release from the header of the answer
// when it has one, and otherwise from version.number. The header is read
// before the status, because the answer to a user that GET / refuses carries
// it too.
func (c *Connector) release(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/", nil)
	if err != nil {
		return "", fmt.Errorf("making the request for the release: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return "", err
	}
	if release := releaseOf(res.Header); release != "" {
		res.Body.Close()
		return release, nil
	}
	if err := checkStatus(res); err != nil {
		return "", err
	}
	defer res.Body.Close()
	var info struct {
		Version struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxRelease), &info); err != nil {
		return "", fmt.Errorf("reading the release: %w", err)
	}
	if info.Version.Number == "" {
		return "", fmt.Errorf("reading the release: the answer holds none: %w", dbimp.ErrInvalidValue)
	}
	return info.Version.Number, nil
}

// releaseOf returns the release that the header X-OpenSearch-Version names,
// such as "3.9.0", and "" when the header is absent or has another form.
func releaseOf(h http.Header) string {
	rest, ok := strings.CutPrefix(h.Get(releaseHeader), "OpenSearch/")
	if !ok {
		return ""
	}
	release, _, _ := strings.Cut(rest, " ")
	return release
}
