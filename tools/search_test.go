package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// ddgRedirectURL is a DDG redirect URL used in test fixtures. Split from the HTML
// constant to avoid exceeding the lll line-length limit.
const ddgRedirectURL = "//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&rut=abc"

// sampleDDGLiteHTML is a minimal DuckDuckGo Lite HTML response with two results.
var sampleDDGLiteHTML = `<html><body><table>` +
	`<tr><td><a class="result-link" href="https://example.com">Example Site</a></td></tr>` +
	`<tr><td class="result-snippet">A sample snippet for example.com</td></tr>` +
	`<tr><td><a class="result-link" href="` + ddgRedirectURL + `">Go Language</a></td></tr>` +
	`<tr><td class="result-snippet">The Go programming language</td></tr>` +
	`</table></body></html>`

func TestParseLiteSearchResults(t *testing.T) {
	results, err := parseLiteSearchResults(sampleDDGLiteHTML, 10)
	require.NoError(t, err)

	require.Len(t, results, 2)

	assert.Equal(t, "Example Site", results[0].Title)
	assert.Equal(t, "https://example.com", results[0].Link)
	assert.Equal(t, "A sample snippet for example.com", results[0].Snippet)
	assert.Equal(t, 1, results[0].Position)

	assert.Equal(t, "Go Language", results[1].Title)
	assert.Equal(t, "https://golang.org/", results[1].Link) // DDG redirect cleaned
	assert.Equal(t, "The Go programming language", results[1].Snippet)
	assert.Equal(t, 2, results[1].Position)
}

func TestParseLiteSearchResults_MaxResults(t *testing.T) {
	results, err := parseLiteSearchResults(sampleDDGLiteHTML, 1)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Example Site", results[0].Title)
}

func TestParseLiteSearchResults_Empty(t *testing.T) {
	results, err := parseLiteSearchResults("<html><body></body></html>", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestCleanDuckDuckGoURL_Redirect(t *testing.T) {
	raw := "//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&rut=abc"
	got := cleanDuckDuckGoURL(raw)
	assert.Equal(t, "https://golang.org/", got)
}

func TestCleanDuckDuckGoURL_Direct(t *testing.T) {
	direct := "https://example.com"
	got := cleanDuckDuckGoURL(direct)
	assert.Equal(t, direct, got)
}

func TestHasClass(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<a class="result-link foo">text</a>`))
	require.NoError(t, err)

	// Find the <a> node
	var aNode *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			aNode = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	require.NotNil(t, aNode)

	assert.True(t, hasClass(aNode, "result-link"))
	assert.True(t, hasClass(aNode, "foo"))
	assert.False(t, hasClass(aNode, "bar"))
}

func TestGetTextContent(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<div>Hello <b>World</b>!</div>`))
	require.NoError(t, err)

	// Find div
	var divNode *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			divNode = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	require.NotNil(t, divNode)

	text := getTextContent(divNode)
	assert.Equal(t, "Hello World!", text)
}

func TestFormatSearchResults_Empty(t *testing.T) {
	out := formatSearchResults(nil)
	assert.Equal(t, "No results found. Try rephrasing your search.", out)
}

func TestFormatSearchResults_WithResults(t *testing.T) {
	results := []SearchResult{
		{Title: "Foo", Link: "https://foo.com", Snippet: "foo snippet", Position: 1},
	}
	out := formatSearchResults(results)
	assert.Contains(t, out, "Foo")
	assert.Contains(t, out, "https://foo.com")
	assert.Contains(t, out, "foo snippet")
	assert.Contains(t, out, "Found 1 search results")
}

func TestWalkHTMLTree_EarlyExit(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<ul><li>a</li><li>b</li><li>c</li></ul>`))
	require.NoError(t, err)

	count := 0
	walkHTMLTree(doc, func(n *html.Node) bool {
		count++
		return count < 3 // stop after visiting 3 nodes
	})
	assert.Equal(t, 3, count)
}
