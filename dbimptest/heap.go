package dbimptest

import (
	"runtime"
	"testing"
)

// HeapLimit is the growth of the live heap that a driver can cause while it
// reads a result of 64 MiB, as D25 requires.
const HeapLimit = 16 << 20

// HeapGauge measures how much the live heap of the process grows while a
// driver reads a large result. It reads HeapAlloc after a full collection, so
// that garbage that the collector has not yet freed, the free slots inside a
// span, and the memory that earlier tests left live do not count. A driver
// that holds the whole result still shows the whole result.
//
// A test that uses a HeapGauge must not run in parallel with another test.
type HeapGauge struct {
	base uint64
	peak uint64
}

// NewHeapGauge returns a HeapGauge whose base is the live heap now. Call it
// after the test has opened its database and before it reads the first row.
func NewHeapGauge() *HeapGauge {
	return &HeapGauge{base: liveHeap()}
}

// Sample collects garbage and records the growth of the live heap since the
// base, if it is the largest growth so far.
func (g *HeapGauge) Sample() {
	if live := liveHeap(); live > g.base {
		g.peak = max(g.peak, live-g.base)
	}
}

// Peak returns the largest growth in bytes that Sample recorded.
func (g *HeapGauge) Peak() uint64 {
	return g.peak
}

// Check fails the test if the largest growth is over HeapLimit.
func (g *HeapGauge) Check(t *testing.T) {
	t.Helper()
	if g.peak > HeapLimit {
		t.Errorf("the live heap grew by %d bytes while the driver read a result of 64 MiB, want at most %d bytes (D25)", g.peak, HeapLimit)
	}
}

// liveHeap returns the bytes of the objects that are live after a full
// collection. It collects twice, so that an object that only a finalizer or
// a goroutine that has just ended held is also freed.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}
