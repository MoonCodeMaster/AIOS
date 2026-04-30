package cli

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

const banner = `
   █████╗ ██╗ ██████╗ ███████╗
  ██╔══██╗██║██╔═══██╗██╔════╝
  ███████║██║██║   ██║███████╗
  ██╔══██║██║██║   ██║╚════██║
  ██║  ██║██║╚██████╔╝███████║
  ╚═╝  ╚═╝╚═╝ ╚═════╝ ╚══════╝`

// printBanner writes the AIOS banner once to w.
func printBanner(w io.Writer) {
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	boldCyan := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	dim := lipgloss.NewStyle().Faint(true)

	fmt.Fprintln(w, cyan.Render(banner))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  %s\n",
		boldCyan.Render("Dual-AI project orchestrator"),
		dim.Render("v"+Version))
	fmt.Fprintln(w)
}
