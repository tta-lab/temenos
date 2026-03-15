package tools

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	goldmarktext "github.com/yuin/goldmark/text"
)

// mdHeading holds metadata for one parsed markdown heading.
type mdHeading struct {
	level  int
	text   string
	offset int    // byte offset of the heading line in source
	id     string // 2-char base62 ID assigned by assignIDs
}

// DefaultTreeThreshold is the character count above which markdown content returns a heading tree by default.
const DefaultTreeThreshold = 5000

// maxContentChars is the maximum character count before content is truncated.
const maxContentChars = 30_000

// truncateContent truncates content to maxContentChars runes.
func truncateContent(s string) string {
	if utf8.RuneCountInString(s) <= maxContentChars {
		return s
	}
	return string([]rune(s)[:maxContentChars]) + "\n[content truncated at 30,000 characters]"
}

// base62Chars is the character set for ID generation.
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// toBase62 encodes n as a base62 string of at least minLen characters.
func toBase62(n uint64, minLen int) string {
	if n == 0 {
		return strings.Repeat("0", minLen)
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Chars[n%62]
		n /= 62
	}
	s := string(buf[i:])
	for len(s) < minLen {
		s = "0" + s
	}
	return s
}

// headingHash returns a stable 64-bit hash of a heading content string.
func headingHash(heading string) uint64 {
	h := sha256.Sum256([]byte(heading))
	var n uint64
	for _, b := range h[:8] {
		n = n<<8 | uint64(b)
	}
	return n
}

// parseHeadings parses markdown source and returns all headings with levels and byte offsets.
func parseHeadings(source []byte) []mdHeading { //nolint:gocyclo
	md := goldmark.New()
	reader := goldmarktext.NewReader(source)
	doc := md.Parser().Parse(reader)

	var headings []mdHeading
	if err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		// Collect heading text (strip inline markup).
		var textBuf strings.Builder
		for c := h.FirstChild(); c != nil; c = c.NextSibling() {
			if seg, ok := c.(*ast.Text); ok {
				textBuf.Write(seg.Segment.Value(source))
			} else if code, ok := c.(*ast.CodeSpan); ok {
				for cc := code.FirstChild(); cc != nil; cc = cc.NextSibling() {
					if seg, ok := cc.(*ast.Text); ok {
						textBuf.Write(seg.Segment.Value(source))
					}
				}
			}
		}

		offset := 0
		if h.Lines() != nil && h.Lines().Len() > 0 {
			offset = h.Lines().At(0).Start
		} else if seg := n.Lines(); seg != nil && seg.Len() > 0 {
			offset = seg.At(0).Start
		}

		headings = append(headings, mdHeading{
			level:  h.Level,
			text:   textBuf.String(),
			offset: offset,
		})
		return ast.WalkContinue, nil
	}); err != nil {
		slog.Warn("parseHeadings: ast.Walk returned an error", "error", err)
	}

	// Fix offsets: for ATX headings, goldmark gives the content start.
	// We need to walk back to find the '#' characters.
	for i := range headings {
		off := headings[i].offset
		// Walk backward from off to find start of line (the '#' chars).
		for off > 0 && source[off-1] != '\n' {
			off--
		}
		headings[i].offset = off
	}

	return headings
}

// assignIDs generates stable 2-char base62 IDs for each heading.
// H1 headings get no ID (they can't be targeted with --section).
// On collision (same hash), extends to 3 chars with positional disambiguator.
func assignIDs(headings []mdHeading) {
	used := map[string]int{} // id → first-use index

	for i := range headings {
		if headings[i].level == 1 {
			headings[i].id = ""
			continue
		}
		hash := headingHash(headings[i].text)
		id := toBase62(hash, 2)

		// Resolve collisions by extending length.
		attempt := 0
		for {
			if prev, exists := used[id]; !exists || prev == i {
				break
			}
			attempt++
			// Mix in position to disambiguate.
			id = toBase62(hash^uint64(attempt)<<32|uint64(i), 3)
		}
		used[id] = i
		headings[i].id = id
	}
}

// extractSection returns the byte slice from the target heading to the next
// heading at the same or higher level (lower number), or end of document.
func extractSection(source []byte, headings []mdHeading, sectionID string) (string, error) {
	targetIdx := -1
	for i, h := range headings {
		if h.id == sectionID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		var ids []string
		for _, h := range headings {
			if h.id != "" {
				ids = append(ids, fmt.Sprintf("%q (%s)", h.id, h.text))
			}
		}
		return "", fmt.Errorf("section %q not found; available: %s", sectionID, strings.Join(ids, ", "))
	}

	target := headings[targetIdx]
	start := target.offset

	// Find end: next heading at same or higher level.
	end := len(source)
	for _, h := range headings[targetIdx+1:] {
		if h.level <= target.level {
			end = h.offset
			break
		}
	}

	return strings.TrimRight(string(source[start:end]), "\n") + "\n", nil
}

// sectionCharCount returns the character count of a section's content.
func sectionCharCount(source []byte, headings []mdHeading, idx int) int {
	start := headings[idx].offset
	end := len(source)
	for _, h := range headings[idx+1:] {
		if h.level <= headings[idx].level {
			end = h.offset
			break
		}
	}
	return utf8.RuneCount(source[start:end])
}

// renderTree builds an indented tree of headings with IDs and char counts.
// Includes a hint line at the end.
func renderTree(headings []mdHeading, source []byte) string {
	if len(headings) == 0 {
		return "(no headings)\n"
	}

	var sb strings.Builder

	// Print H1 title if present.
	if headings[0].level == 1 {
		charCount := sectionCharCount(source, headings, 0)
		fmt.Fprintf(&sb, "# %s\n\nTotal: %s characters\n\n", headings[0].text, formatNum(charCount))
	}

	minLevel := 99
	for _, h := range headings {
		if h.level > 1 && h.level < minLevel {
			minLevel = h.level
		}
	}
	if minLevel == 99 {
		minLevel = 2
	}

	// Track stack for tree connectors.
	type stackEntry struct{ level int }
	var stack []stackEntry

	for i, h := range headings {
		if h.level == 1 {
			continue
		}

		// Pop stack entries that are deeper than current level.
		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}

		depth := h.level - minLevel
		indent := strings.Repeat("│   ", depth)

		// Determine connector.
		connector := "├─ "
		// Check if this is the last sibling at this level.
		isLast := true
		for _, future := range headings[i+1:] {
			if future.level <= h.level {
				isLast = future.level < h.level
				break
			}
		}
		if isLast {
			connector = "└─ "
		}

		charCount := sectionCharCount(source, headings, i)
		idStr := ""
		if h.id != "" {
			idStr = fmt.Sprintf("[%s] ", h.id)
		}
		fmt.Fprintf(&sb, "%s%s%s%s %s (%s chars)\n",
			indent, connector, idStr,
			strings.Repeat("#", h.level), h.text,
			formatNum(charCount),
		)

		stack = append(stack, stackEntry{level: h.level})
	}

	sb.WriteString("\nUse section: \"<id>\" to read a specific section, or full: true to read everything.\n")
	return sb.String()
}

// formatNum formats an integer with thousands separators.
func formatNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
