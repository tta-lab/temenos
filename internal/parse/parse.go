package parse

import (
	"strings"
)

// Command represents a parsed command from a text block.
type Command struct {
	Args string // everything after the prefix (e.g. "ls -la")
}

// ParseBlock splits a block of text on Barrier (§) lines and returns each command.
// Everything between § lines becomes one command.
// Lines before the first § are ignored.
func ParseBlock(block string) []Command {
	lines := strings.Split(block, "\n")
	var cmds []Command
	var current strings.Builder
	collecting := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, Barrier) {
			// Flush previous command
			if current.Len() > 0 {
				if cmd := strings.TrimSpace(current.String()); cmd != "" {
					cmds = append(cmds, Command{Args: cmd})
				}
				current.Reset()
			}
			// Start collecting for next command
			collecting = true
			args := strings.TrimPrefix(trimmed, Barrier)
			args = strings.TrimPrefix(args, " ")
			current.WriteString(args)
		} else if collecting {
			// Continue building current command
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(line)
		}
		// else: non-§ line before first § - ignore
	}

	// Flush final command
	if current.Len() > 0 {
		if cmd := strings.TrimSpace(current.String()); cmd != "" {
			cmds = append(cmds, Command{Args: cmd})
		}
	}

	return cmds
}
