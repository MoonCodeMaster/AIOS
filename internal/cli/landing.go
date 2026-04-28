package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const landingCard = `You're not in an AIOS repo.

  • Run ` + "`aios init`" + ` here to bootstrap one
  • Or cd to an existing AIOS repo and run ` + "`aios`" + ` again

Run ` + "`aios --help`" + ` for the full command list.
`

// printLandingCard writes the landing message to w. Returned for testability.
func printLandingCard(w io.Writer) {
	fmt.Fprint(w, landingCard)
}

// hasAIOSConfig reports whether the current cwd has a .aios/config.toml.
// Used by bare-aios to decide between landing card and REPL launch.
func hasAIOSConfig() bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(wd, ".aios", "config.toml"))
	return err == nil
}
