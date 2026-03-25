package cli

import (
	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/internal/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the temenos MCP server (stdio transport)",
	Long: `Start the temenos MCP stdio server.

Claude Code can spawn this process to use the sandbox as an MCP tool provider.
The server exposes a single "bash" tool that executes commands in the temenos
sandbox, proxying requests to the running temenos daemon.

Configuration via environment variables:
  TEMENOS_WRITE=true        Allow read-write access to the working directory (default: read-only)
  TEMENOS_SOCKET_PATH=...   Override the temenos daemon socket path
  TTAL_SOCKET_PATH=...      Override the ttal daemon socket path

Example .mcp.json:
  {
    "mcpServers": {
      "temenos": {
        "type": "stdio",
        "command": "temenos",
        "args": ["mcp"],
        "env": { "TEMENOS_WRITE": "true" }
      }
    }
  }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcp.Serve(Version)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
