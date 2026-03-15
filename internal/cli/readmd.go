package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/tools"
)

var readMDFlags struct {
	tree          bool
	section       string
	full          bool
	treeThreshold int
}

var readMDCmd = &cobra.Command{
	Use:   "read-md <file>",
	Short: tools.ReadMDCommand.Summary,
	Long:  tools.ReadMDCommand.Help,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := tools.ReadMarkdown(
			args[0], readMDFlags.tree, readMDFlags.section, readMDFlags.full, readMDFlags.treeThreshold,
		)
		if err != nil {
			return err
		}
		fmt.Print(result.Content)
		return nil
	},
}

func init() {
	readMDCmd.Flags().BoolVar(&readMDFlags.tree, "tree", false, "Force tree view")
	readMDCmd.Flags().StringVar(&readMDFlags.section, "section", "", "Section ID to extract")
	readMDCmd.Flags().BoolVar(&readMDFlags.full, "full", false, "Force full content")
	readMDCmd.Flags().IntVar(&readMDFlags.treeThreshold, "tree-threshold",
		tools.DefaultTreeThreshold, "Char count for auto tree mode")
	rootCmd.AddCommand(readMDCmd)
}
