package cli

import "testing"

func TestParsePRRef_AcceptedShapes(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"42", true, "42"},
		{"https://github.com/owner/repo/pull/42", true, "https://github.com/owner/repo/pull/42"},
		{"http://github.com/owner/repo/pull/42", true, "http://github.com/owner/repo/pull/42"},
		{"owner/repo#42", true, "owner/repo#42"},
		{"  42  ", true, "42"},
		{"", false, ""},
		{"not a pr", false, ""},
	}
	for _, c := range cases {
		got, err := parsePRRef(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parsePRRef(%q): err=%v want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("parsePRRef(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestRepoFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo/pull/42":      "owner/repo",
		"https://github.com/MoonCodeMaster/AIOS/pull/1": "MoonCodeMaster/AIOS",
		"https://example.com/owner/repo/pull/42":     "",
		"":                                            "",
	}
	for in, want := range cases {
		if got := repoFromURL(in); got != want {
			t.Errorf("repoFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
