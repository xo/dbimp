package avatica //nolint:testpackage // The fake transport replaces the transport of the connector, which is not exported.

import (
	"bytes"
	"database/sql"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// localTransport answers each call of Avatica that answers names with the
// answer that it names, and sends every other call to next. The shared
// tests serve one body for every request, and the recordings hold no
// closeStatement, so a test answers these calls here.
type localTransport struct {
	next    http.RoundTripper
	answers map[string]string
}

func (lt localTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var m struct {
		Request string `json:"request"`
	}
	_ = json.Unmarshal(b, &m)
	if ans, ok := lt.answers[m.Request]; ok {
		status := http.StatusOK
		if strings.Contains(ans, `"response":"error"`) {
			status = http.StatusInternalServerError
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(ans)),
			Request:    req,
		}, nil
	}
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(b))
	out.ContentLength = int64(len(b))
	return lt.next.RoundTrip(out)
}

// setupAnswers are the answers of the calls that open a connection, make a
// statement and close each, in the form of the server (measured).
var setupAnswers = map[string]string{
	"openConnection":  `{"response":"openConnection"}`,
	"connectionSync":  `{"response":"connectionSync","connProps":{"connProps":"connPropsImpl","autoCommit":true,"readOnly":false,"transactionIsolation":2,"dirty":false}}`,
	"createStatement": `{"response":"createStatement","connectionId":"c","statementId":1}`,
	"closeStatement":  `{"response":"closeStatement"}`,
	"closeConnection": `{"response":"closeConnection"}`,
	"commit":          `{"response":"commit"}`,
	"rollback":        `{"response":"rollback"}`,
}

// openLocal opens a database on the server at url, whose connector answers
// the calls of answers itself.
func openLocal(t *testing.T, url string, answers map[string]string) *sql.DB {
	t.Helper()
	cfg, err := ParseDSN(strings.Replace(url, "http://", "avatica://", 1))
	if err != nil {
		t.Fatal(err)
	}
	c := NewConnector(*cfg)
	c.client = dbimp.NewClient(localTransport{next: c.transport, answers: answers}, false)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return db
}
