package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tta-lab/temenos/tools"
)

var readURLFlags struct {
	tree          bool
	section       string
	full          bool
	treeThreshold int
	gatewayURL    string
	cacheDir      string
}

var readURLCmd = &cobra.Command{
	Use:   "read-url <url>",
	Short: tools.ReadURLCommand.Summary,
	Long:  tools.ReadURLCommand.Help,
	Args:  cobra.ExactArgs(1),
	RunE:  runReadURL,
}

func runReadURL(cmd *cobra.Command, args []string) error {
	backend, err := resolveBackend()
	if err != nil {
		return err
	}

	markdown, err := backend.Fetch(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("fetch %s: %w", args[0], err)
	}

	result, err := tools.RenderMarkdownContent(
		[]byte(markdown), readURLFlags.tree, readURLFlags.section, readURLFlags.full, readURLFlags.treeThreshold,
	)
	if err != nil {
		return err
	}
	fmt.Print(result.Content)
	return nil
}

// resolveBackend selects a fetch backend in priority order:
// 1. defuddle (if on PATH) — best local extraction quality
// 2. BROWSER_GATEWAY_URL (or --gateway-url) — server-side extraction
// 3. Error — neither available; user must install one or the other.
func resolveBackend() (tools.ReadURLBackend, error) {
	cacheDir := readURLFlags.cacheDir
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not determine home directory, using /tmp for cache: %v\n", err)
			home = "/tmp"
		}
		cacheDir = filepath.Join(home, ".cache", "temenos", "scrapes")
	}

	if _, err := exec.LookPath("defuddle"); err == nil {
		return tools.NewCachedFetchBackend(cacheDir, tools.NewDefuddleCLIBackend()), nil
	}

	gwURL := readURLFlags.gatewayURL
	if gwURL == "" {
		gwURL = os.Getenv("BROWSER_GATEWAY_URL")
	}
	if gwURL != "" {
		return tools.NewCachedFetchBackend(cacheDir, tools.NewBrowserGatewayBackend(gwURL, nil)), nil
	}

	return nil, fmt.Errorf("no fetch backend: install defuddle or set BROWSER_GATEWAY_URL")
}

func init() {
	readURLCmd.Flags().BoolVar(&readURLFlags.tree, "tree", false, "Force tree view")
	readURLCmd.Flags().StringVar(&readURLFlags.section, "section", "", "Section ID to extract")
	readURLCmd.Flags().BoolVar(&readURLFlags.full, "full", false, "Force full content")
	readURLCmd.Flags().IntVar(&readURLFlags.treeThreshold, "tree-threshold",
		tools.DefaultTreeThreshold, "Char count for auto tree mode")
	readURLCmd.Flags().StringVar(&readURLFlags.gatewayURL, "gateway-url", "", "Browser gateway URL")
	readURLCmd.Flags().StringVar(&readURLFlags.cacheDir, "cache-dir", "", "Cache directory (default ~/.cache/temenos/scrapes)") //nolint:lll
	rootCmd.AddCommand(readURLCmd)
}
