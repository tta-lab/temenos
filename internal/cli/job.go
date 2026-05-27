package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/client"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage background jobs",
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List background jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewJobClient()
		if err != nil {
			return err
		}

		status, _ := cmd.Flags().GetString("status")
		callerID, _ := cmd.Flags().GetString("caller-id")

		jobs, err := c.ListJobs(context.Background(), callerID, status)
		if err != nil {
			return err
		}

		if len(jobs) == 0 {
			fmt.Println("no jobs found")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tCOMMAND\tSTATUS\tSTARTED")
		for _, j := range jobs {
			cmd := j.Command
			if len(cmd) > 40 {
				cmd = cmd[:37] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", j.ID, cmd, j.Status, j.StartedAt)
		}
		return w.Flush()
	},
}

var jobLogCmd = &cobra.Command{
	Use:   "log <id>",
	Short: "Show output of a background job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewJobClient()
		if err != nil {
			return err
		}

		job, err := c.GetJob(context.Background(), args[0])
		if err != nil {
			return err
		}

		if job.Stdout != "" {
			fmt.Printf("[stdout]\n%s\n", job.Stdout)
		}
		if job.Stderr != "" {
			fmt.Printf("[stderr]\n%s\n", job.Stderr)
		}
		if job.Status == "completed" || job.Status == "killed" {
			fmt.Printf("(exit code: %d, status: %s)\n", job.ExitCode, job.Status)
		} else {
			fmt.Printf("(status: %s)\n", job.Status)
		}
		return nil
	},
}

var jobKillCmd = &cobra.Command{
	Use:   "kill <id>",
	Short: "Kill a running background job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewJobClient()
		if err != nil {
			return err
		}

		job, err := c.KillJob(context.Background(), args[0])
		if err != nil {
			return err
		}

		fmt.Printf("killed job %s (%s)\n", job.ID, job.Command)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(jobCmd)
	jobCmd.AddCommand(jobListCmd)
	jobCmd.AddCommand(jobLogCmd)
	jobCmd.AddCommand(jobKillCmd)

	jobListCmd.Flags().StringP("status", "s", "running", "Filter by status (running, completed, all)")
	jobListCmd.Flags().StringP("caller-id", "c", "", "Filter by caller ID")
}
