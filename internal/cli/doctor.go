package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/sandbox"
)

// currentStatus is injectable for tests.
var currentStatus = sandbox.CurrentStatus

// doctorNotReadyErr is returned when the sandbox runtime is not ready.
var doctorNotReadyErr = &doctorNotReadyError{}

type doctorNotReadyError struct{}

func (e *doctorNotReadyError) Error() string { return "sandbox not ready" }

var doctorCmd = &cobra.Command{
	Use:               "doctor",
	Short:             "Diagnose sandbox runtime (cgroup v2, k8s, memory delegation)",
	SilenceUsage:      true,
	SilenceErrors:     true,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		status := currentStatus()
		if cmd.Flag("json").Value.String() == "true" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(status); err != nil {
				return fmt.Errorf("write JSON output: %w", err)
			}
		} else {
			if _, err := io.WriteString(cmd.OutOrStdout(), status.String()+"\n"); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
		}
		if !status.Ready {
			return doctorNotReadyErr
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("json", false, "output as JSON")
}
