package cli

import (
	"errors"
	"os/exec"
	"testing"
)

func TestAutopilotPreflight_GhMissing(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "", errors.New("not found") },
		runCmd:    func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return true, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "gh") {
		t.Errorf("err = %v, want one mentioning 'gh'", err)
	}
}

func TestAutopilotPreflight_GhAuthBroken(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return errors.New("auth: not logged in") },
		hasRemote: func() (bool, error) { return true, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "gh auth") {
		t.Errorf("err = %v, want one mentioning 'gh auth'", err)
	}
}

func TestAutopilotPreflight_NoRemote(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return false, nil },
	}
	err := pre.Check()
	if err == nil || !contains(err.Error(), "remote") {
		t.Errorf("err = %v, want one mentioning 'remote'", err)
	}
}

func TestAutopilotPreflight_HappyPath(t *testing.T) {
	pre := &autopilotPreflight{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/gh", nil },
		runCmd:    func(*exec.Cmd) error { return nil },
		hasRemote: func() (bool, error) { return true, nil },
	}
	if err := pre.Check(); err != nil {
		t.Errorf("happy path returned err: %v", err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || index(s, sub) >= 0) }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
