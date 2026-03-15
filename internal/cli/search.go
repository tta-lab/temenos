package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/tools"
)

var searchFlags struct {
	maxResults int
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: tools.SearchCommand.Summary,
	Long:  tools.SearchCommand.Help,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := tools.Search(context.Background(), args[0], searchFlags.maxResults)
		if err != nil {
			return err
		}
		fmt.Print(result)
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVarP(&searchFlags.maxResults, "max-results", "n", 10, "Maximum number of results (max 20)")
	rootCmd.AddCommand(searchCmd)
}
