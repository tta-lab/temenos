Read a markdown file with structure awareness.

Flags:
  --tree           Show heading tree with section IDs and char counts
  --section ID     Extract a specific section by ID (get IDs from --tree)
  --full           Force full content regardless of size

Large files (>5000 chars) auto-show tree view. Use --section to drill in.

Examples:
  logos read-md README.md --tree
  logos read-md README.md --section 3K
