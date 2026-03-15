Fetch a URL and return as clean markdown.

Flags:
  --tree           Show heading tree with section IDs
  --section ID     Extract a specific section by ID
  --full           Force full content

Strips navigation, ads, scripts. Results cached daily.

Examples:
  logos read-url https://go.dev/doc/effective_go --tree
  logos read-url https://go.dev/doc/effective_go --section aZ
