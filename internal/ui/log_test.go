package ui

import (
	"strings"
	"testing"
)

func TestStep(t *testing.T) {
	out, errOut := withGlobal(t, Config{}, func() { Step("downloading %s", "rg") })
	if !strings.Contains(out, "→ downloading rg") {
		t.Errorf("Step: stdout = %q, want it to contain the message", out)
	}
	if errOut != "" {
		t.Errorf("Step: stderr = %q, want empty", errOut)
	}
}

func TestStepQuiet(t *testing.T) {
	out, _ := withGlobal(t, Config{Quiet: true}, func() { Step("downloading %s", "rg") })
	if out != "" {
		t.Errorf("Step with Quiet: stdout = %q, want empty", out)
	}
}

func TestStepJSONGoesToStderr(t *testing.T) {
	out, errOut := withGlobal(t, Config{JSON: true}, func() { Step("downloading rg") })
	if out != "" {
		t.Errorf("Step with JSON: stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "downloading rg") {
		t.Errorf("Step with JSON: stderr = %q, want it to contain the message", errOut)
	}
}

func TestOK(t *testing.T) {
	out, _ := withGlobal(t, Config{}, func() { OK("installed %s", "rg") })
	if !strings.Contains(out, "✓ installed rg") {
		t.Errorf("OK: stdout = %q, want it to contain the message", out)
	}
}

func TestOKQuiet(t *testing.T) {
	out, _ := withGlobal(t, Config{Quiet: true}, func() { OK("installed rg") })
	if out != "" {
		t.Errorf("OK with Quiet: stdout = %q, want empty", out)
	}
}

func TestOKJSONGoesToStderr(t *testing.T) {
	out, errOut := withGlobal(t, Config{JSON: true}, func() { OK("installed rg") })
	if out != "" {
		t.Errorf("OK with JSON: stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "installed rg") {
		t.Errorf("OK with JSON: stderr = %q, want it to contain the message", errOut)
	}
}

func TestOKField(t *testing.T) {
	out, _ := withGlobal(t, Config{}, func() { OKField("path", "/opt/bin/rg") })
	if !strings.Contains(out, "path:") || !strings.Contains(out, "/opt/bin/rg") {
		t.Errorf("OKField: stdout = %q, want it to contain label and value", out)
	}
}

func TestOKFieldQuiet(t *testing.T) {
	out, _ := withGlobal(t, Config{Quiet: true}, func() { OKField("path", "/opt/bin/rg") })
	if out != "" {
		t.Errorf("OKField with Quiet: stdout = %q, want empty", out)
	}
}

func TestWarnField(t *testing.T) {
	_, errOut := withGlobal(t, Config{}, func() { WarnField("path", "/opt/bin/rg", "not on PATH") })
	for _, want := range []string{"path:", "/opt/bin/rg", "not on PATH"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("WarnField: stderr = %q, want it to contain %q", errOut, want)
		}
	}
}

func TestFail(t *testing.T) {
	_, errOut := withGlobal(t, Config{}, func() { Fail("download failed: %s", "timeout") })
	if !strings.Contains(errOut, "✗ download failed: timeout") {
		t.Errorf("Fail: stderr = %q, want it to contain the message", errOut)
	}
}

func TestWarn(t *testing.T) {
	_, errOut := withGlobal(t, Config{}, func() { Warn("no verification configured") })
	if !strings.Contains(errOut, "! no verification configured") {
		t.Errorf("Warn: stderr = %q, want it to contain the message", errOut)
	}
}

func TestHint(t *testing.T) {
	_, errOut := withGlobal(t, Config{}, func() { Hint("run paq doctor") })
	if !strings.Contains(errOut, "hint: run paq doctor") {
		t.Errorf("Hint: stderr = %q, want it to contain the message", errOut)
	}
}

func TestHintQuiet(t *testing.T) {
	_, errOut := withGlobal(t, Config{Quiet: true}, func() { Hint("run paq doctor") })
	if errOut != "" {
		t.Errorf("Hint with Quiet: stderr = %q, want empty", errOut)
	}
}

func TestInfoHiddenByDefault(t *testing.T) {
	out, errOut := withGlobal(t, Config{}, func() { Info("resolved url %s", "https://example.com") })
	if out != "" || errOut != "" {
		t.Errorf("Info without --verbose/--debug: stdout=%q stderr=%q, want both empty", out, errOut)
	}
}

func TestInfoVerbose(t *testing.T) {
	out, _ := withGlobal(t, Config{Verbose: true}, func() { Info("resolved url %s", "https://example.com") })
	if !strings.Contains(out, "resolved url https://example.com") {
		t.Errorf("Info with Verbose: stdout = %q, want it to contain the message", out)
	}
}

func TestInfoJSONGoesToStderr(t *testing.T) {
	out, errOut := withGlobal(t, Config{Verbose: true, JSON: true}, func() { Info("resolved url") })
	if out != "" {
		t.Errorf("Info with JSON: stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, "resolved url") {
		t.Errorf("Info with JSON: stderr = %q, want it to contain the message", errOut)
	}
}

func TestDebugHiddenWithoutFlag(t *testing.T) {
	_, errOut := withGlobal(t, Config{}, func() { Debug("temp dir %s", "/tmp/paq-1") })
	if errOut != "" {
		t.Errorf("Debug without --debug: stderr = %q, want empty", errOut)
	}
}

func TestDebugEnabled(t *testing.T) {
	_, errOut := withGlobal(t, Config{Debug: true}, func() { Debug("temp dir %s", "/tmp/paq-1") })
	if !strings.Contains(errOut, "[debug] temp dir /tmp/paq-1") {
		t.Errorf("Debug with --debug: stderr = %q, want it to contain the message", errOut)
	}
}
