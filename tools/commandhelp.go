package tools

import _ "embed"

//go:embed commands/read_url.md
var readURLHelp string

//go:embed commands/search.md
var searchHelp string

//go:embed commands/rg.md
var rgHelp string

// CommandHelp holds help text for a command. Single source of truth —
// used by both cobra (--help) and the system prompt builder.
type CommandHelp struct {
	Name    string // display name, e.g. "temenos read-url", "rg"
	Summary string // one-line description (cobra Short)
	Help    string // full help text (cobra Long AND system prompt)
}

var (
	ReadURLCommand = CommandHelp{
		Name:    "temenos read-url",
		Summary: "Fetch a URL and return as clean markdown",
		Help:    readURLHelp,
	}
	SearchCommand = CommandHelp{
		Name:    "temenos search",
		Summary: "Search the web via DuckDuckGo",
		Help:    searchHelp,
	}
	RGCommand = CommandHelp{
		Name:    "rg",
		Summary: "Search file contents (ripgrep)",
		Help:    rgHelp,
	}
)

// AllCommands is the full set of available commands.
// Temenos ships: read-url, search (custom binaries) + rg (external, help text only).
// Standard shell tools (sed, cat, wc, bash) are on PATH — no custom binary needed.
var AllCommands = []CommandHelp{
	ReadURLCommand, SearchCommand, RGCommand,
}

// SelectCommands returns CommandHelp entries matching the given names.
// Names not found are silently skipped.
func SelectCommands(names ...string) []CommandHelp {
	result := make([]CommandHelp, 0, len(names))
	for _, name := range names {
		for _, c := range AllCommands {
			if c.Name == name {
				result = append(result, c)
				break
			}
		}
	}
	return result
}
