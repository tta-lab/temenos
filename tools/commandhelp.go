package tools

import _ "embed"

//go:embed commands/read_url.md
var readURLHelp string

//go:embed commands/search.md
var searchHelp string

// CommandHelp holds help text for a command. Single source of truth —
// used by both cobra (--help) and the system prompt builder.
type CommandHelp struct {
	Name    string // display name, e.g. "temenos read-url"
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
)

// AllCommands is the full set of available commands.
// Temenos ships: read-url, search (custom binaries).
// Standard shell tools (sed, cat, wc, rg) are on PATH — no custom binary needed.
var AllCommands = []CommandHelp{
	ReadURLCommand, SearchCommand,
}
