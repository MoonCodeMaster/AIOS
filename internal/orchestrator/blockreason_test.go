package orchestrator

import "testing"

func TestBlockReason_String(t *testing.T) {
	cases := []struct {
		name string
		br   BlockReason
		want string
	}{
		{"code only", BlockReason{Code: CodeMaxRoundsExceeded}, "max_rounds_exceeded"},
		{"code + detail", BlockReason{Code: CodeStallNoProgress, Detail: "3 rounds"}, "stall_no_progress: 3 rounds"},
		{"upstream only", BlockReason{Code: CodeUpstreamBlocked, Upstream: "T1"}, "upstream_blocked:T1"},
		{"upstream + detail", BlockReason{Code: CodeUpstreamBlocked, Upstream: "T1", Detail: "chain"}, "upstream_blocked:T1: chain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.br.String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNewUpstreamBlock(t *testing.T) {
	br := NewUpstreamBlock("A")
	if br.Code != CodeUpstreamBlocked {
		t.Errorf("Code = %s, want %s", br.Code, CodeUpstreamBlocked)
	}
	if br.Upstream != "A" {
		t.Errorf("Upstream = %q, want A", br.Upstream)
	}
}
