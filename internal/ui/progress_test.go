package ui

import (
	"strings"
	"testing"
)

func TestNewProgressFnQuietReturnsNil(t *testing.T) {
	saved := Global
	Global = Config{Quiet: true}
	defer func() { Global = saved }()

	if fn := NewProgressFn("rg"); fn != nil {
		t.Errorf("NewProgressFn with Quiet = %v, want nil", fn)
	}
}

// withStderrTTY makes NewProgressFn see stderr as a terminal (tty=true) or
// not, for the duration of the test.
func withStderrTTY(t *testing.T, tty bool) {
	t.Helper()
	saved := stderrIsTTY
	stderrIsTTY = func() bool { return tty }
	t.Cleanup(func() { stderrIsTTY = saved })
}

func TestNewProgressFnNoTTYReturnsNil(t *testing.T) {
	withStderrTTY(t, false)
	saved := Global
	Global = Config{}
	defer func() { Global = saved }()

	if fn := NewProgressFn("rg"); fn != nil {
		t.Errorf("NewProgressFn without a TTY = %v, want nil", fn)
	}
}

func TestNewProgressFnKnownTotal(t *testing.T) {
	withStderrTTY(t, true)
	_, errOut := withGlobal(t, Config{}, func() {
		fn := NewProgressFn("rg")
		fn(50, 100)
		fn(50, 100) // duplicate percentage: must not print again
		fn(100, 100)
	})

	if !strings.Contains(errOut, "rg: 50%") || !strings.Contains(errOut, "rg: 100%") {
		t.Errorf("NewProgressFn output = %q, want it to contain 50%% and 100%% once each", errOut)
	}
	if strings.Count(errOut, "50%") != 1 {
		t.Errorf("NewProgressFn output = %q, want the duplicate 50%% update suppressed", errOut)
	}
}

func TestNewProgressFnUnknownTotal(t *testing.T) {
	withStderrTTY(t, true)
	_, errOut := withGlobal(t, Config{}, func() {
		fn := NewProgressFn("rg")
		fn(1024*1024, 0)
	})

	if !strings.Contains(errOut, "rg: 1 MB downloaded") {
		t.Errorf("NewProgressFn with unknown total = %q, want it to report MB downloaded", errOut)
	}
}
