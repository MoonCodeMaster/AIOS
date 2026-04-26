package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		in       string
		maj, min int
		ok       bool
	}{
		{"git version 2.40.1", 2, 40, true},
		{"git version 2.39.5 (Apple Git-153)", 2, 39, true},
		{"git version 2.42.0.windows.1", 2, 42, true},
		{"git version 1.9.0", 1, 9, true},
		{"unknown 3.0", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		maj, min, ok := parseGitVersion(c.in)
		if maj != c.maj || min != c.min || ok != c.ok {
			t.Errorf("parseGitVersion(%q) = (%d,%d,%v); want (%d,%d,%v)", c.in, maj, min, ok, c.maj, c.min, c.ok)
		}
	}
}

func TestDoctorReport_RequiredFailFailsRun(t *testing.T) {
	var buf bytes.Buffer
	d := newDoctor(&buf)
	d.add(check{Name: "claude on PATH", Status: statusPass, Required: true})
	d.add(check{Name: "codex on PATH", Status: statusFail, Detail: "not found", Required: true})
	if d.report() {
		t.Error("report() = true; want false (a required FAIL must fail the run)")
	}
	out := buf.String()
	if !strings.Contains(out, "[FAIL] codex on PATH") {
		t.Errorf("output missing FAIL row:\n%s", out)
	}
	if !strings.Contains(out, "blocked: fix FAIL rows") {
		t.Errorf("output missing blocked banner:\n%s", out)
	}
}

func TestDoctorReport_NonRequiredFailDowngradesToWarn(t *testing.T) {
	var buf bytes.Buffer
	d := newDoctor(&buf)
	d.add(check{Name: "claude on PATH", Status: statusPass, Required: true})
	d.add(check{Name: "gh on PATH", Status: statusFail, Detail: "missing", Required: false})
	if !d.report() {
		t.Error("report() = false; non-required FAIL should not fail the run")
	}
	out := buf.String()
	if !strings.Contains(out, "[WARN] gh on PATH") {
		t.Errorf("non-required FAIL should render as WARN; got:\n%s", out)
	}
	if !strings.Contains(out, "ready for `aios run`") {
		t.Errorf("output missing ready-with-warnings banner:\n%s", out)
	}
}

func TestDoctorReport_AllPassReadyBanner(t *testing.T) {
	var buf bytes.Buffer
	d := newDoctor(&buf)
	d.add(check{Name: "git installed", Status: statusPass, Required: true})
	d.add(check{Name: "claude on PATH", Status: statusPass, Required: true})
	if !d.report() {
		t.Error("report() = false; all passing should succeed")
	}
	if !strings.Contains(buf.String(), "ready: all required checks passed.") {
		t.Errorf("output missing ready banner:\n%s", buf.String())
	}
}

func TestDoctor_AddDowngradesNonRequiredFail(t *testing.T) {
	var buf bytes.Buffer
	d := newDoctor(&buf)
	d.add(check{Name: "x", Status: statusFail, Required: false})
	if d.checks[0].Status != statusWarn {
		t.Errorf("non-required FAIL was stored as %v; want WARN", d.checks[0].Status)
	}
}
