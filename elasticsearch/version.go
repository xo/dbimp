package elasticsearch

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// maxRelease is the most of the answer to GET / that the driver reads. The
// answer is one small object (measured).
const maxRelease = 64 << 10

// version answers SELECT version(), which SQL in Elasticsearch has no
// function for (D181). It reads version.number from GET /, which gives it to
// the administrator, and gives HTTP 403 to a user that lacks the privilege
// cluster:monitor/main. The driver passes that error on as it is.
func (c *conn) version(ctx context.Context) (driver.Rows, error) {
	release, err := c.c.release(ctx)
	if err != nil {
		return nil, err
	}
	return dbimp.NewVersionRows(release), nil
}

// release sends GET / and returns version.number.
func (c *Connector) release(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/", nil)
	if err != nil {
		return "", fmt.Errorf("making the request for the release: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	c.setAuth(req)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return "", err
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
