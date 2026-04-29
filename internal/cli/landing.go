package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const banner = `
   █████╗ ██╗ ██████╗ ███████╗
  ██╔══██╗██║██╔═══██╗██╔════╝
  ███████║██║██║   ██║███████╗
  ██╔══██║██║██║   ██║╚════██║
  ██║  ██║██║╚██████╔╝███████║
  ╚═╝  ╚═╝╚═╝ ╚═════╝ ╚══════╝`

// printLandingCard writes the landing message to w.
func printLandingCard(w io.Writer) {
	cCyan.Fprintln(w, banner)
	fmt.Fprintln(w)
	cDim.Fprintf(w, "  Dual-AI project orchestrator  %s\n", cDim.Sprint("v"+Version))
	fmt.Fprintln(w)
	printWarn(w, "You're not in an AIOS repo.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  Bootstrap a new project here\n", cBoldCyan.Sprint("aios init"))
	fmt.Fprintf(w, "  %s      One-shot preflight check\n", cBoldCyan.Sprint("aios doctor"))
	fmt.Fprintf(w, "  %s    Full command reference\n", cBoldCyan.Sprint("aios --help"))
	fmt.Fprintln(w)
	cDim.Fprintln(w, "  Or cd to an existing AIOS repo and run `aios` again.")
	fmt.Fprintln(w)
}

type landingCardKey struct{}

func markLandingCard(ctx context.Context) context.Context {
	return context.WithValue(ctx, landingCardKey{}, true)
}

func landingCardPrinted(ctx context.Context) bool {
	v, _ := ctx.Value(landingCardKey{}).(bool)
	return v
}

func hasAIOSConfig() bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(wd, ".aios", "config.toml"))
	return err == nil
}
