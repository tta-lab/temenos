package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHeadings_Basic(t *testing.T) {
	src := []byte("# H1\n\nsome text\n\n## H2\n\nmore\n\n### H3\n\ndone\n")
	headings := parseHeadings(src)
	require.Len(t, headings, 3)
	assert.Equal(t, 1, headings[0].level)
	assert.Equal(t, "H1", headings[0].text)
	assert.Equal(t, 2, headings[1].level)
	assert.Equal(t, "H2", headings[1].text)
	assert.Equal(t, 3, headings[2].level)
	assert.Equal(t, "H3", headings[2].text)
}

func TestParseHeadings_InlineCode(t *testing.T) {
	src := []byte("## The `foo` Function\n\ncontent\n")
	headings := parseHeadings(src)
	require.Len(t, headings, 1)
	assert.Contains(t, headings[0].text, "foo")
}

func TestParseHeadings_Empty(t *testing.T) {
	src := []byte("no headings here\n")
	headings := parseHeadings(src)
	assert.Empty(t, headings)
}

func TestAssignIDs_UniqueHeadings(t *testing.T) {
	src := []byte("## Alpha\n\n## Beta\n\n## Gamma\n")
	headings := parseHeadings(src)
	assignIDs(headings)
	ids := map[string]bool{}
	for _, h := range headings {
		assert.NotEmpty(t, h.id, "id should be set for %q", h.text)
		assert.False(t, ids[h.id], "duplicate id %q for heading %q", h.id, h.text)
		ids[h.id] = true
	}
}

func TestAssignIDs_CollisionHandling(t *testing.T) {
	// Two headings with the same text should get different IDs.
	src := []byte("## Same\n\ntext\n\n## Same\n\nmore text\n")
	headings := parseHeadings(src)
	assignIDs(headings)
	require.Len(t, headings, 2)
	assert.NotEqual(t, headings[0].id, headings[1].id, "duplicate headings should get unique IDs")
}

func TestExtractSection_Basic(t *testing.T) {
	src := []byte("# Title\n\n## Intro\n\nintro text\n\n## Details\n\ndetail text\n")
	headings := parseHeadings(src)
	assignIDs(headings)

	// Find the Intro section ID
	var introID string
	for _, h := range headings {
		if h.text == "Intro" {
			introID = h.id
		}
	}
	require.NotEmpty(t, introID)

	section, err := extractSection(src, headings, introID)
	require.NoError(t, err)
	assert.Contains(t, section, "## Intro")
	assert.Contains(t, section, "intro text")
	assert.NotContains(t, section, "detail text")
}

func TestExtractSection_LastSection(t *testing.T) {
	src := []byte("## First\n\nfirst text\n\n## Last\n\nlast text\n")
	headings := parseHeadings(src)
	assignIDs(headings)

	var lastID string
	for _, h := range headings {
		if h.text == "Last" {
			lastID = h.id
		}
	}

	section, err := extractSection(src, headings, lastID)
	require.NoError(t, err)
	assert.Contains(t, section, "## Last")
	assert.Contains(t, section, "last text")
}

func TestExtractSection_NotFound(t *testing.T) {
	src := []byte("## Alpha\n\ntext\n")
	headings := parseHeadings(src)
	assignIDs(headings)

	_, err := extractSection(src, headings, "ZZ")
	assert.Error(t, err)
}

func TestRenderTree_Format(t *testing.T) {
	src := []byte("# RenderDoc\n\n## Section A\n\ncontent of a\n\n## Section B\n\ncontent of b\n")
	headings := parseHeadings(src)
	assignIDs(headings)

	tree := renderTree(headings, src)

	// Should have section IDs and char counts
	assert.Contains(t, tree, "Section A")
	assert.Contains(t, tree, "Section B")
	assert.Contains(t, tree, "chars")
	// Should have hint line
	assert.Contains(t, tree, "section:")
	assert.Contains(t, tree, "full: true")
}

func TestRenderTree_NestedHeadings(t *testing.T) {
	src := []byte("# Top\n\n## Section\n\ntext\n\n### Subsection\n\nsubtext\n")
	headings := parseHeadings(src)
	assignIDs(headings)
	tree := renderTree(headings, src)

	lines := strings.Split(tree, "\n")
	// ### should be more indented than ##
	var sectionLine, subsectionLine string
	for _, l := range lines {
		if strings.Contains(l, "Section") && !strings.Contains(l, "Sub") {
			sectionLine = l
		}
		if strings.Contains(l, "Subsection") {
			subsectionLine = l
		}
	}
	assert.NotEmpty(t, sectionLine)
	assert.NotEmpty(t, subsectionLine)
	// Subsection line should be more indented
	sectionIndent := len(sectionLine) - len(strings.TrimLeft(sectionLine, " │├└"))
	subsectionIndent := len(subsectionLine) - len(strings.TrimLeft(subsectionLine, " │├└"))
	assert.Greater(t, subsectionIndent, sectionIndent)
}

func TestTruncateContent_Short(t *testing.T) {
	assert.Equal(t, "hello", truncateContent("hello"))
}

func TestTruncateContent_Long(t *testing.T) {
	big := strings.Repeat("x", 200_000)
	result := truncateContent(big)
	assert.Less(t, len(result), len(big))
	assert.Contains(t, result, "truncated")
}
