package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/sandbox"
)

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Sandbox management and diagnostics",
}

var sandboxStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sandbox environment status",
	RunE: func(cmd *cobra.Command, args []string) error {
		status := sandbox.CurrentStatus()
		if cmd.Flag("json").Value.String() == "true" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(status)
		}
		fmt.Fprintln(cmd.OutOrStdout(), status.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sandboxCmd)
	sandboxCmd.AddCommand(sandboxStatusCmd)
	sandboxStatusCmd.Flags().Bool("json", false, "output as JSON")
}
