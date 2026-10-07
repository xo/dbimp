package drill //nolint:testpackage // The test builds a watch in the state that a race of the transport leaves, which is not exported.

import (
	"context"
	"testing"
)

// TestEndCancelsAfterTheContextEnded holds D165: when the context ended, and
// the transport saw it before the function of the watch started, stop returns
// true and the function never runs. end must send the cancel itself, because
// the query still runs on the server.
func TestEndCancelsAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ended error
		want  int
	}{
		{"the context ended", context.Canceled, 1},
		{"the context lives", nil, 0},
	} {
		sent := 0
		w := &watch{
			done: make(chan struct{}),
			stop: func() bool { return true },
			err:  func() error { return tt.ended },
			now:  func() { sent++ },
		}
		w.end()
		if sent != tt.want {
			t.Errorf("%s: end sent %d cancels, want %d", tt.name, sent, tt.want)
		}
	}
}
