package neo4j //nolint:testpackage // The test builds a watch in the state that a race of the transport leaves, which is not exported.

import (
	"context"
	"testing"
)

// TestEndStopsAfterTheContextEnded holds D67: when the context ended, and the
// transport saw it before the function of the watch started, stop returns
// true and the function never runs. end must send the stop itself, because
// the statement still runs on the server.
func TestEndStopsAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ended error
		want  int
	}{
		{"the context ended", context.Canceled, 1},
		{"the context lives", nil, 0},
	} {
		stopped := 0
		w := &watch{
			done:  make(chan struct{}),
			stop:  func() bool { return true },
			ended: func() error { return tt.ended },
			now:   func() error { stopped++; return nil },
		}
		ran, err := w.end()
		if err != nil || ran != (tt.want == 1) || stopped != tt.want {
			t.Errorf("%s: end gave %v, %v and sent %d stops, want %d", tt.name, ran, err, stopped, tt.want)
		}
	}
}
