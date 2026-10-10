package dbimptest_test

import (
	"runtime"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// TestHeapGauge makes sure that the gauge sees a result that a driver holds in
// memory, and does not see garbage that the driver dropped.
func TestHeapGauge(t *testing.T) {
	gauge := dbimptest.NewHeapGauge()
	for range 64 {
		_ = make([]byte, 1<<20)
		gauge.Sample()
	}
	if got := gauge.Peak(); got > dbimptest.HeapLimit {
		t.Errorf("garbage raised the peak to %d bytes, want at most %d", got, dbimptest.HeapLimit)
	}
	held := make([][]byte, 0, 64)
	for range 64 {
		held = append(held, make([]byte, 1<<20))
		gauge.Sample()
	}
	if got := gauge.Peak(); got <= dbimptest.HeapLimit {
		t.Errorf("a held result of 64 MiB gave a peak of %d bytes, want over %d", got, dbimptest.HeapLimit)
	}
	runtime.KeepAlive(held)
}
