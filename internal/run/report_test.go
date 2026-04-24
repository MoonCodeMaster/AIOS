package run

import (
	"strings"
	"testing"
)

func TestRenderReport_Blocked(t *testing.T) {
	rpt := Report{
		TaskID: "002-login",
		Status: "blocked",
		Reason: "max_rounds_exceeded",
		Rounds: []Round{
			{N: 1, DiffLines: 20, VerifyGreen: false, ReviewApproved: false,
				UnmetCriteria: []string{"POST /login 401 coverage"}},
			{N: 2, DiffLines: 5, VerifyGreen: true, ReviewApproved: false,
				UnmetCriteria: []string{"POST /login 401 coverage"}},
		},
		UsageTokens: 18250,
	}
	md := RenderReport(rpt)
	if !strings.Contains(md, "002-login") || !strings.Contains(md, "max_rounds_exceeded") {
		t.Errorf("report missing key info: %s", md)
	}
	if !strings.Contains(md, "POST /login 401 coverage") {
		t.Errorf("report missing unmet criteria: %s", md)
	}
}
