package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

const banner = `
   █████╗ ██╗ ██████╗ ███████╗
  ██╔══██╗██║██╔═══██╗██╔════╝
  ███████║██║██║   ██║███████╗
  ██╔══██║██║██║   ██║╚════██║
  ██║  ██║██║╚██████╔╝███████║
  ╚═╝  ╚═╝╚═╝ ╚═════╝ ╚══════╝`

// printLandingCard writes the landing message to w using lipgloss styling.
func printLandingCard(w io.Writer) {
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	boldCyan := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	dim := lipgloss.NewStyle().Faint(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	fmt.Fprintln(w, cyan.Render(banner))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  %s\n",
		boldCyan.Render("Dual-AI project orchestrator"),
		dim.Render("v"+Version))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s You're not in an AIOS repo.\n", warn.Render("⚠"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  Bootstrap a new project here\n", boldCyan.Render("aios init"))
	fmt.Fprintf(w, "  %s      One-shot preflight check\n", boldCyan.Render("aios doctor"))
	fmt.Fprintf(w, "  %s    Full command reference\n", boldCyan.Render("aios --help"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim.Render("  Or cd to an existing AIOS repo and run `aios` again."))
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
