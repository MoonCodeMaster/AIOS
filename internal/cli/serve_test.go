package cli

import (
	"strings"
	"testing"
)

func TestServeCmdHelpDescribesContract(t *testing.T) {
	c := newServeCmd()
	long := c.Long
	for _, want := range []string{"aios:do", "GitHub", "label", "--once"} {
		if !strings.Contains(long, want) {
			t.Errorf("serve --help missing %q; got: %s", want, long)
		}
	}
}
