package cli

import "github.com/spf13/cobra"

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "temenos",
	Short:   "Sandbox daemon for AI agents",
	Version: Version,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
