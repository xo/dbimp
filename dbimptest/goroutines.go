package dbimptest

import (
	"bytes"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// goroutineWait is how long CheckGoroutines waits for a goroutine to end.
const goroutineWait = 5 * time.Second

// CheckGoroutines fails the test if a goroutine that started during the
// test is still running when the test ends, as step 12 of docs/DRIVER.md
// requires. Call it first in the test, so that it runs after every other
// cleanup, such as the one that closes the database. It counts every
// goroutine of the process, so the test that calls it must not run in
// parallel with another test.
func CheckGoroutines(t *testing.T) {
	t.Helper()
	before := goroutineIDs()
	t.Cleanup(func() {
		var left []string
		for deadline := time.Now().Add(goroutineWait); ; {
			left = newGoroutines(before)
			if len(left) == 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		for _, stack := range left {
			t.Errorf("a goroutine is still running after the test:\n%s", stack)
		}
	})
}

// goroutineIDs returns the id of every goroutine of the process.
func goroutineIDs() map[int]bool {
	ids := make(map[int]bool)
	for _, stack := range stacks() {
		ids[stackID(stack)] = true
	}
	return ids
}

// newGoroutines returns the stack of every goroutine that is not in before,
// except the goroutines of the testing package itself.
func newGoroutines(before map[int]bool) []string {
	var left []string
	for _, stack := range stacks() {
		if before[stackID(stack)] || isTesting(stack) {
			continue
		}
		left = append(left, stack)
	}
	return left
}

func stacks() []string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var out []string
	for stack := range bytes.SplitSeq(buf, []byte("\n\n")) {
		out = append(out, string(stack))
	}
	return out
}

func stackID(stack string) int {
	rest, ok := bytes.CutPrefix([]byte(stack), []byte("goroutine "))
	if !ok {
		return -1
	}
	num, _, ok := bytes.Cut(rest, []byte(" "))
	if !ok {
		return -1
	}
	id, err := strconv.Atoi(string(num))
	if err != nil {
		return -1
	}
	return id
}

// isTesting reports whether the goroutine belongs to the testing package,
// such as a goroutine that runs a test or waits for one. A goroutine that a
// driver starts has none of these in its stack.
func isTesting(stack string) bool {
	return slices.ContainsFunc([]string{
		"testing.tRunner",
		"testing.runTests",
		"testing.(*M).",
		"testing.(*T).Run",
	}, func(s string) bool {
		return strings.Contains(stack, s)
	})
}
