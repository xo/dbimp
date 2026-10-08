package neo4j //nolint:testpackage // The test builds rows in the state that a race of the transport leaves, which is not exported.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// TestReadErrorAfterTheContextEnded holds D36: when the transport closes the
// connection before the read sees that the context ended, the error of the rows
// still says that the context ended.
func TestReadErrorAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ended error
		want  error
	}{
		{"the context ended", context.Canceled, context.Canceled},
		{"the context lives", nil, net.ErrClosed},
	} {
		body := io.MultiReader(
			strings.NewReader(`{"data":{"fields":["a"],"values":[[{"$type":"Integer","_value":"1"}],[`),
			&failingReader{err: net.ErrClosed},
		)
		res := &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Body: io.NopCloser(body)}
		r, err := readResponse(res, nil)
		if err != nil {
			t.Fatalf("%s: reading the head gave %v", tt.name, err)
		}
		r.ended = func() error { return tt.ended }
		var got error
		for got == nil {
			got = r.NextRow()
		}
		if !errors.Is(got, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, got, tt.want)
		}
	}
}

// failingReader fails every read with err.
type failingReader struct{ err error }

func (r *failingReader) Read([]byte) (int, error) { return 0, r.err }
