package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// `aios mcp scaffold <preset>` appends a ready-to-use MCP server block to
// .aios/config.toml. The presets cover the integrations users hit first —
// GitHub, the local filesystem, Playwright — so the path from "I want
// MCP" to "MCP is working" is one command instead of an hour of docs
// reading.
//
// The command is non-destructive: it appends to the file, never rewrites
// existing blocks, and refuses to add a duplicate preset.
func newMCPCmd() *cobra.Command {
	c := &cobra.Command{
		Use:         "mcp",
		Short:       "Manage MCP server configuration",
		Annotations: map[string]string{gateAnnotation: gateLevelAIOS},
	}
	c.AddCommand(newMCPScaffoldCmd())
	c.AddCommand(newMCPListCmd())
	return c
}

func newMCPScaffoldCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "scaffold <preset>",
		Short: "Append a ready MCP server block to .aios/config.toml",
		Long: `aios mcp scaffold appends a known-good [mcp.servers.<name>] block to
.aios/config.toml so you do not have to write the TOML by hand.

Available presets:
  github        github-mcp-server (env: GITHUB_TOKEN)
  fs-readonly   read-only local filesystem (mcp-fs --read-only)
  playwright    headless browser via @playwright/mcp

Run "aios mcp list" to see preset bodies before scaffolding. Existing
blocks are not modified — re-running with the same preset reports
"already present" and exits 0.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPScaffold(args[0])
		},
	}
	return c
}

func newMCPListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available MCP scaffold presets",
		RunE: func(cmd *cobra.Command, args []string) error {
			names := make([]string, 0, len(mcpPresets))
			for k := range mcpPresets {
				names = append(names, k)
			}
			sort.Strings(names)
			out := cmd.OutOrStdout()
			for _, n := range names {
				fmt.Fprintf(out, "## %s\n", n)
				fmt.Fprintln(out, mcpPresets[n])
			}
			return nil
		},
	}
}

// mcpPresets maps preset name → TOML block. Each block defines exactly
// one [mcp.servers.<name>] section plus a small comment header explaining
// what the integration does and what env vars / external services it
// expects. The user is expected to read the comment before relying on
// the preset.
var mcpPresets = map[string]string{
	"github": `# github — GitHub Search and Issues via the official MCP server.
# Install: npm install -g @modelcontextprotocol/server-github
# Required env: GITHUB_TOKEN (a fine-grained PAT with repo scope is enough).
[mcp.servers.github]
binary = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = { GITHUB_PERSONAL_ACCESS_TOKEN = "${env:GITHUB_TOKEN}" }
allowed_tools = [
  "search_repositories",
  "search_code",
  "get_issue",
  "list_issues",
  "get_pull_request",
  "list_pull_requests",
]
`,
	"fs-readonly": `# fs-readonly — read-only access to the current repo via mcp-fs.
# Install: npm install -g @modelcontextprotocol/server-filesystem
# This is a safe default for letting the model browse code without write risk.
[mcp.servers.fs-readonly]
binary = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "--read-only", "."]
allowed_tools = ["read_file", "list_directory", "search_files"]
`,
	"playwright": `# playwright — headless browser control for end-to-end checks and scraping.
# Install: npm install -g @playwright/mcp
# Required: 'npx playwright install chromium' once on this machine.
[mcp.servers.playwright]
binary = "npx"
args = ["-y", "@playwright/mcp"]
allowed_tools = ["browser_navigate", "browser_click", "browser_type", "browser_screenshot"]
`,
}

func runMCPScaffold(preset string) error {
	body, ok := mcpPresets[preset]
	if !ok {
		known := make([]string, 0, len(mcpPresets))
		for k := range mcpPresets {
			known = append(known, k)
		}
		sort.Strings(known)
		return fmt.Errorf("unknown preset %q (known: %s)", preset, strings.Join(known, ", "))
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(wd, ".aios", "config.toml")
	existing, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s — run `aios init` first: %w", cfgPath, err)
	}
	header := fmt.Sprintf("[mcp.servers.%s]", preset)
	if strings.Contains(string(existing), header) {
		fmt.Printf("mcp scaffold %s: already present in %s; nothing changed.\n", preset, cfgPath)
		return nil
	}
	if err := appendBlock(cfgPath, body); err != nil {
		return err
	}
	fmt.Printf("mcp scaffold %s: appended to %s\n", preset, cfgPath)
	fmt.Printf("Next: read the comment block, set any required env vars, then opt your tasks in via:\n\n  mcp_allow: [%s]\n\n", preset)
	return nil
}

func appendBlock(path, block string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + block); err != nil {
		return err
	}
	return nil
}

// errPresetUnknown is returned when an unknown preset is requested. Kept
// as a sentinel so future callers (e.g. a `dev/test` mode that wants to
// fail loudly) can match against it.
var errPresetUnknown = errors.New("mcp: unknown preset")
