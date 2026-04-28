package cli

import (
	"context"
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

type landingCardKey struct{}

// markLandingCard puts a marker on the context indicating the landing card
// has already been printed by PersistentPreRunE. RunE checks this and exits
// early when set, instead of running the REPL launch logic.
func markLandingCard(ctx context.Context) context.Context {
	return context.WithValue(ctx, landingCardKey{}, true)
}

// landingCardPrinted reports whether markLandingCard was called for this ctx.
func landingCardPrinted(ctx context.Context) bool {
	v, _ := ctx.Value(landingCardKey{}).(bool)
	return v
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
