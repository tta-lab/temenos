package parse

import "strings"

// Command represents a parsed command from a text block.
type Command struct {
	Raw  string // full original line (e.g. "§ ls -la")
	Args string // everything after the prefix (e.g. "ls -la")
}

// isHeredocClose reports whether line closes a heredoc with the given delimiter.
// Handles plain close (TrimSpace) and <<- indented close (TrimRight tabs).
func isHeredocClose(line, delim string) bool {
	return strings.TrimSpace(line) == delim || strings.TrimRight(line, "\t") == delim
}

// heredocDelimiter extracts the delimiter from a heredoc operator in a command string.
// Handles: <<EOF, <<'EOF', <<"EOF", <<-EOF, <<-'EOF', <<- 'EOF'
// Returns the delimiter and true if found, or empty string and false if no heredoc.
func heredocDelimiter(cmdArgs string) (string, bool) {
	idx := strings.Index(cmdArgs, "<<")
	if idx == -1 {
		return "", false
	}

	rest := cmdArgs[idx+2:]
	rest = strings.TrimPrefix(rest, "-")
	rest = strings.TrimSpace(rest)

	if rest == "" {
		return "", false
	}

	// Strip surrounding quotes if present
	if strings.HasPrefix(rest, "'") && strings.Contains(rest[1:], "'") {
		end := strings.Index(rest[1:], "'") + 1
		return rest[1:end], true
	}
	if strings.HasPrefix(rest, "\"") && strings.Contains(rest[1:], "\"") {
		end := strings.Index(rest[1:], "\"") + 1
		return rest[1:end], true
	}

	// Unquoted: take until whitespace or shell metachar
	delim := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '|' || r == '&' || r == ')'
	})
	if len(delim) == 0 {
		return "", false
	}
	return delim[0], true
}

// parseCommand checks if a line starts with the given prefix.
// Returns the command and true if matched, zero Command and false otherwise.
func parseCommand(line, prefix string) (Command, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, prefix) {
		return Command{}, false
	}
	args := strings.TrimPrefix(trimmed, prefix)
	if args == "" {
		return Command{}, false
	}
	return Command{
		Raw:  trimmed,
		Args: args,
	}, true
}

// ParseBlock scans a block of text for commands marked with the given prefix.
// It handles heredoc accumulation: when a command contains a heredoc operator
// (<<), subsequent lines are collected until the closing delimiter, and the
// entire multi-line string becomes the command's Args.
//
// The prefix is caller-defined (e.g. "§ "). temenos does not hardcode any
// particular prefix.
func ParseBlock(block, prefix string) []Command {
	lines := strings.Split(block, "\n")
	var cmds []Command
	var heredocDelim string

	for i, line := range lines {
		if heredocDelim != "" {
			if isHeredocClose(line, heredocDelim) {
				heredocDelim = ""
			}
			continue
		}

		c, ok := parseCommand(line, prefix)
		if !ok {
			continue
		}

		delim, hasHeredoc := heredocDelimiter(c.Args)
		if !hasHeredoc {
			cmds = append(cmds, c)
			continue
		}

		// Heredoc: accumulate lines until closing delimiter.
		var body strings.Builder
		body.WriteString(c.Args)
		heredocDelim = delim
		for j := i + 1; j < len(lines); j++ {
			body.WriteString("\n" + lines[j])
			if isHeredocClose(lines[j], delim) {
				heredocDelim = ""
				break
			}
		}
		cmds = append(cmds, Command{Raw: c.Raw, Args: body.String()})
	}

	return cmds
}
