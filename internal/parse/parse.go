package parse

import "strings"

// Command represents a parsed command from a text block.
type Command struct {
	Args string // everything after the prefix (e.g. "ls -la")
}

// heredocSpec holds the closing delimiter and whether this is a <<- form.
type heredocSpec struct {
	delim  string
	isDash bool // true for <<- (leading tabs stripped from delimiter line)
}

// isHeredocClose reports whether line closes a heredoc described by spec.
// For <<-: leading tabs are stripped before comparing (POSIX tab-stripping).
// For <<: line must match the delimiter exactly.
func isHeredocClose(line string, spec heredocSpec) bool {
	if spec.isDash {
		return strings.TrimLeft(line, "\t") == spec.delim
	}
	return line == spec.delim
}

// parseHeredocSpec extracts the heredoc spec from a command string.
// Handles: <<EOF, <<'EOF', <<"EOF", <<-EOF, <<-'EOF', <<- 'EOF'
// Returns the spec and true if found, or zero spec and false if no heredoc.
func parseHeredocSpec(cmdArgs string) (heredocSpec, bool) {
	idx := strings.Index(cmdArgs, "<<")
	if idx == -1 {
		return heredocSpec{}, false
	}

	rest := cmdArgs[idx+2:]
	isDash := strings.HasPrefix(rest, "-")
	rest = strings.TrimPrefix(rest, "-")
	rest = strings.TrimSpace(rest)

	if rest == "" {
		return heredocSpec{}, false
	}

	// Strip surrounding quotes if present
	if strings.HasPrefix(rest, "'") && strings.Contains(rest[1:], "'") {
		end := strings.Index(rest[1:], "'") + 1
		return heredocSpec{delim: rest[1:end], isDash: isDash}, true
	}
	if strings.HasPrefix(rest, "\"") && strings.Contains(rest[1:], "\"") {
		end := strings.Index(rest[1:], "\"") + 1
		return heredocSpec{delim: rest[1:end], isDash: isDash}, true
	}

	// Unquoted: take until whitespace or shell metachar
	parts := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '|' || r == '&' || r == ')'
	})
	if len(parts) == 0 {
		return heredocSpec{}, false
	}
	return heredocSpec{delim: parts[0], isDash: isDash}, true
}

// parseCommandArgs checks if a line starts with the given prefix.
// Returns the args string (everything after the prefix) and true if matched,
// or empty string and false otherwise.
func parseCommandArgs(line, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, prefix) {
		return "", false
	}
	args := strings.TrimPrefix(trimmed, prefix)
	if args == "" {
		return "", false
	}
	return args, true
}

// ParseBlock scans a block of text for commands marked with the given prefix.
// It handles heredoc accumulation: when a command contains a heredoc operator
// (<<), subsequent lines are collected until the closing delimiter, and the
// entire multi-line string becomes the command's Args.
//
// Uses an index-based loop so that heredoc body lines are skipped by advancing
// the index past the closing delimiter — preventing body lines from being
// re-evaluated as commands.
//
// The prefix is caller-defined (e.g. "§ "). temenos does not hardcode any
// particular prefix.
func ParseBlock(block, prefix string) []Command {
	lines := strings.Split(block, "\n")
	var cmds []Command

	for i := 0; i < len(lines); i++ {
		args, ok := parseCommandArgs(lines[i], prefix)
		if !ok {
			continue
		}

		spec, hasHeredoc := parseHeredocSpec(args)
		if !hasHeredoc {
			cmds = append(cmds, Command{Args: args})
			continue
		}

		// Heredoc: advance i through body lines until closing delimiter.
		var body strings.Builder
		body.WriteString(args)
		for i++; i < len(lines); i++ {
			body.WriteString("\n" + lines[i])
			if isHeredocClose(lines[i], spec) {
				break
			}
		}
		cmds = append(cmds, Command{Args: body.String()})
	}

	return cmds
}
