package tools

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

const (
	maxFileBytes     = 5 * 1024 * 1024 // 5MB
	defaultReadLines = 2000
	maxLineChars     = 2000
)

// readTextFile reads lines [offset, offset+limit) from the file.
// offset is 0-based. Returns the raw lines (without line numbers).
func readTextFile(path string, offset, limit int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var lines []string
	lineNum := 0
	for scanner.Scan() {
		if lineNum >= offset && len(lines) < limit {
			line := scanner.Text()
			// Truncate extremely long lines.
			if len([]rune(line)) > maxLineChars {
				line = string([]rune(line)[:maxLineChars]) + " [line truncated]"
			}
			lines = append(lines, line)
		}
		lineNum++
		if len(lines) >= limit {
			break
		}
	}
	return lines, scanner.Err()
}

// addLineNumbers formats lines with 1-based line numbers starting at startLine.
// Format: "     1\tline content"
func addLineNumbers(lines []string, startLine int) string {
	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%6d\t%s\n", startLine+i, line)
	}
	return sb.String()
}

// normalizeMaxResults clamps maxResults to the [1, 20] range, defaulting to 10.
func normalizeMaxResults(n int) int {
	if n <= 0 {
		return 10
	}
	if n > 20 {
		return 20
	}
	return n
}
