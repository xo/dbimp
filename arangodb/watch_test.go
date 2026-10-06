package arangodb //nolint:testpackage // The test builds a watch in the state that a race of the transport leaves, which is not exported.

import (
	"context"
	"testing"
)

// TestEndKillsAfterTheContextEnded holds D90: when the context ended, and the
// transport saw it before the function of the watch started, stop returns
// true and the function never runs. end must kill the query itself, because
// it still runs on the server.
func TestEndKillsAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ended error
		want  int
	}{
		{"the context ended", context.Canceled, 1},
		{"the context lives", nil, 0},
	} {
		killed := 0
		w := &watch{
			done:  make(chan struct{}),
			stop:  func() bool { return true },
			ended: func() error { return tt.ended },
			kill:  func() error { killed++; return nil },
		}
		ran, err := w.end()
		if err != nil || ran != (tt.want == 1) || killed != tt.want {
			t.Errorf("%s: end gave %v, %v and sent %d kills, want %d", tt.name, ran, err, killed, tt.want)
		}
	}
}
