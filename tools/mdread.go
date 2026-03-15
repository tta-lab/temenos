package tools

import (
	"fmt"
	"log/slog"
	"os"
	"unicode/utf8"
)

// MarkdownResult holds the output of a markdown read operation.
type MarkdownResult struct {
	Content string // rendered content (full text, tree, or section)
	Mode    string // "full", "tree", or "section"
}

// RenderMarkdownContent renders markdown source bytes with tree/section/full modes.
// This is the core rendering logic — used by both ReadMarkdown (file) and the
// read-url CLI (in-memory content). No fantasy dependency.
// treeThreshold: auto-switch to tree above this char count (default 5000 if ≤ 0).
func RenderMarkdownContent(
	source []byte, tree bool, section string, full bool, treeThreshold int,
) (*MarkdownResult, error) {
	if treeThreshold <= 0 {
		treeThreshold = DefaultTreeThreshold
	}

	headings := parseHeadings(source)
	assignIDs(headings)

	if section != "" {
		content, err := extractSection(source, headings, section)
		if err != nil {
			return nil, err
		}
		return &MarkdownResult{Content: content, Mode: "section"}, nil
	}

	charCount := utf8.RuneCountInString(string(source))

	if tree || (!full && charCount > treeThreshold) {
		if len(headings) == 0 {
			// No headings — fall back to full content.
			return &MarkdownResult{Content: truncateContent(string(source)), Mode: "full"}, nil
		}
		return &MarkdownResult{Content: renderTree(headings, source), Mode: "tree"}, nil
	}

	return &MarkdownResult{Content: truncateContent(string(source)), Mode: "full"}, nil
}

// ReadMarkdown reads a markdown file with tree/section/full modes.
// Delegates to RenderMarkdownContent after reading the file.
func ReadMarkdown(path string, tree bool, section string, full bool, treeThreshold int) (*MarkdownResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory, not a file", path)
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result, err := RenderMarkdownContent(source, tree, section, full, treeThreshold)
	if err != nil {
		return nil, err
	}

	// Log warning when large file had no headings and fell back to full.
	// Only log when auto-mode triggered the fallback (not explicit --full).
	if result.Mode == "full" && !full && !tree && section == "" {
		charCount := utf8.RuneCountInString(string(source))
		if charCount > treeThreshold {
			slog.Warn("read_md: no headings found, returning full content", "file", path)
		}
	}

	return result, nil
}
