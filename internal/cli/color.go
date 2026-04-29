package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/fatih/color"
)

// Theme colors matching Codex CLI's owo_colors palette.
var (
	cBold    = color.New(color.Bold)
	cCyan    = color.New(color.FgCyan)
	cGreen   = color.New(color.FgGreen)
	cRed     = color.New(color.FgRed)
	cYellow  = color.New(color.FgYellow)
	cDim     = color.New(color.Faint)
	cBoldCyan = color.New(color.Bold, color.FgCyan)
)

// Quiet suppresses non-essential output when true. Set by --quiet flag.
var Quiet bool

// Verbose enables debug-level output when true. Set by --verbose flag.
var Verbose bool

// SetNoColor disables all color output globally.
func SetNoColor(v bool) { color.NoColor = v }

// Themed print helpers — write to the given writer.

func printSuccess(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, cGreen.Sprint("✓ "))
	fmt.Fprintf(w, format, a...)
	fmt.Fprintln(w)
}

func printError(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, cRed.Sprint("✗ "))
	fmt.Fprintf(w, format, a...)
	fmt.Fprintln(w)
}

func printWarn(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, cYellow.Sprint("⚠ "))
	fmt.Fprintf(w, format, a...)
	fmt.Fprintln(w)
}

func printInfo(w io.Writer, format string, a ...any) {
	if Quiet {
		return
	}
	fmt.Fprintf(w, format, a...)
	fmt.Fprintln(w)
}

func printDim(w io.Writer, format string, a ...any) {
	if Quiet {
		return
	}
	cDim.Fprintf(w, format, a...)
	fmt.Fprintln(w)
}

func printDebug(w io.Writer, format string, a ...any) {
	if !Verbose {
		return
	}
	cDim.Fprintf(w, "[debug] "+format, a...)
	fmt.Fprintln(w)
}

// stderrWarn writes a warning to stderr (for non-fatal issues).
func stderrWarn(format string, a ...any) {
	printWarn(os.Stderr, format, a...)
}
