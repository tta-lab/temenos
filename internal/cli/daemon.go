package cli

import (
	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the temenos sandbox daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Run(Version)
	},
}

var daemonInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install temenos as a system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Install()
	},
}

var daemonUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove temenos system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Uninstall()
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if temenos daemon is running",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Status()
	},
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the temenos daemon via service manager",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Start()
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the temenos daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Stop()
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the temenos daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Restart()
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonInstallCmd)
	daemonCmd.AddCommand(daemonUninstallCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
}
