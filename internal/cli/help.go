package cli

import (
	"fmt"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type commandGroup struct {
	Heading string
	Use     []string
}

var rootGroups = []commandGroup{
	{Heading: "Pipeline", Use: []string{"ship", "exec", "run", "serve", "duel", "review"}},
	{Heading: "Setup", Use: []string{"init", "doctor", "mcp", "completion"}},
	{Heading: "Session", Use: []string{"resume"}},
	{Heading: "Inspection", Use: []string{"status", "cost", "lessons", "unblock"}},
}

var installRootHelpOnce sync.Once

func installRootHelp(cmd *cobra.Command) {
	installRootHelpOnce.Do(func() {
		cobra.AddTemplateFunc("rootUsage", rootUsage)
	})
	cmd.SetHelpTemplate(`{{if .HasParent}}{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{else}}{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

{{rootUsage .}}{{end}}`)
}

func rootUsage(c *cobra.Command) string {
	var b strings.Builder

	cBold.Fprint(&b, "Usage:\n")
	fmt.Fprintf(&b, "  %s [prompt]                    Start REPL or run a one-shot prompt\n", cCyan.Sprint("aios"))
	fmt.Fprintf(&b, "  %s <command> [flags]\n\n", cCyan.Sprint("aios"))

	cBold.Fprint(&b, "Session:\n")
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  %s\tPrint pipeline output, no REPL\n", cCyan.Sprint("-p, --print <prompt>"))
	fmt.Fprintf(tw, "  %s\tResume REPL session (latest, or by id)\n", cCyan.Sprint("    --continue [id]"))
	tw.Flush()
	b.WriteString("\n")

	cmds := indexByFirstToken(c.Commands())

	for _, g := range rootGroups {
		cBold.Fprintf(&b, "%s:\n", g.Heading)
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, name := range g.Use {
			sub, ok := cmds[name]
			if !ok {
				continue
			}
			fmt.Fprintf(tw, "  %s\t%s\n", cCyan.Sprint(sub.Use), sub.Short)
		}
		tw.Flush()
		b.WriteString("\n")
	}

	cBold.Fprint(&b, "Flags:\n")
	tw = tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "  %s\tPath to AIOS config (default: ./.aios/config.toml)\n", cCyan.Sprint("    --config <path>"))
	fmt.Fprintf(tw, "  %s\tdebug | info | warn | error  (default: info)\n", cCyan.Sprint("    --log-level <level>"))
	fmt.Fprintf(tw, "  %s\tSuppress progress output\n", cCyan.Sprint("-q, --quiet"))
	fmt.Fprintf(tw, "  %s\tEnable debug-level output\n", cCyan.Sprint("    --verbose"))
	fmt.Fprintf(tw, "  %s\tDisable colored output\n", cCyan.Sprint("    --no-color"))
	fmt.Fprintf(tw, "  %s\tPrint version\n", cCyan.Sprint("-v, --version"))
	fmt.Fprintf(tw, "  %s\tShow help\n", cCyan.Sprint("-h, --help"))
	tw.Flush()
	cDim.Fprintf(&b, "\nRun \"aios <command> --help\" for command-specific help.\n")
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
