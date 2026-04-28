package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// commandGroup labels a set of subcommands by their cobra Use field's first token.
type commandGroup struct {
	Heading string
	Use     []string // first token of cmd.Use ("ship <prompt>" → "ship")
}

// rootGroups defines the grouping shown in `aios --help`.
// New subcommands must be added here to appear in the grouped help; otherwise
// they are still callable but invisible from the top-level help.
var rootGroups = []commandGroup{
	{Heading: "Pipeline", Use: []string{"ship", "run", "serve", "duel", "review"}},
	{Heading: "Setup", Use: []string{"init", "doctor", "mcp"}},
	{Heading: "Inspection", Use: []string{"status", "cost", "lessons", "resume"}},
}

// installRootHelp replaces Cobra's default help template for the root command
// with a grouped layout. Subcommand help is unaffected: Cobra propagates
// help templates from parent → child unless the child sets its own, so the
// template branches on `.HasParent` to fall back to the default rendering for
// any subcommand.
func installRootHelp(cmd *cobra.Command) {
	cobra.AddTemplateFunc("rootUsage", rootUsage)
	cmd.SetHelpTemplate(`{{if .HasParent}}{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{else}}{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

{{rootUsage .}}{{end}}`)
}

func rootUsage(c *cobra.Command) string {
	var b strings.Builder
	b.WriteString("Usage:\n")
	b.WriteString("  aios [prompt]                    Start REPL or run a one-shot prompt\n")
	b.WriteString("  aios <command> [flags]\n\n")

	// Session — flag-driven modes on the root command.
	b.WriteString("Session:\n")
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  -p, --print <prompt>\tPrint pipeline output, no REPL")
	fmt.Fprintln(tw, "      --continue [id]\tResume REPL session (latest, or by id)")
	tw.Flush()
	b.WriteString("\n")

	cmds := indexByFirstToken(c.Commands())

	for _, g := range rootGroups {
		fmt.Fprintf(&b, "%s:\n", g.Heading)
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, name := range g.Use {
			sub, ok := cmds[name]
			if !ok {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\n", sub.Use, sub.Short)
		}
		tw.Flush()
		b.WriteString("\n")
	}

	b.WriteString("Flags:\n")
	tw = tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "      --config <path>\tPath to AIOS config (default: ./.aios/config.toml)")
	fmt.Fprintln(tw, "      --log-level <level>\tdebug | info | warn | error  (default: info)")
	fmt.Fprintln(tw, "  -v, --version\tPrint version")
	fmt.Fprintln(tw, "  -h, --help\tShow help")
	tw.Flush()
	b.WriteString("\nRun \"aios <command> --help\" for command-specific help.\n")
	return b.String()
}

func indexByFirstToken(cmds []*cobra.Command) map[string]*cobra.Command {
	out := make(map[string]*cobra.Command, len(cmds))
	for _, c := range cmds {
		first := strings.SplitN(c.Use, " ", 2)[0]
		out[first] = c
	}
	return out
}
