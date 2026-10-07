package solr

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/xo/dbimp"
)

// maxRelease is the most of the answer to GET /solr/admin/info/system that
// the driver reads. The answer is small (measured).
const maxRelease = 1 << 20

// version answers SELECT version(), which SQL in Solr has no statement for
// (D181). It reads lucene.solr-spec-version from GET /solr/admin/info/system,
// which gives it to the administrator. A user that the role refuses gets the
// error of the request, as every other statement does.
func (c *conn) version(ctx context.Context) (driver.Rows, error) {
	release, err := c.c.release(ctx)
	if err != nil {
		return nil, err
	}
	return dbimp.NewVersionRows(release), nil
}

// release sends GET /solr/admin/info/system and returns
// lucene.solr-spec-version.
func (c *Connector) release(ctx context.Context) (string, error) {
	res, err := c.send(ctx, http.MethodGet, "/solr/admin/info/system", url.Values{"wt": {"json"}}, nil, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var info struct {
		Lucene struct {
			Version string `json:"solr-spec-version"`
		} `json:"lucene"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxRelease), &info); err != nil {
		return "", fmt.Errorf("reading the release: %w", err)
	}
	if info.Lucene.Version == "" {
		return "", fmt.Errorf("reading the release: the answer holds none: %w", dbimp.ErrInvalidValue)
	}
	return info.Lucene.Version, nil
}
