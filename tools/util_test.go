package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "util_test_*.txt")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestReadTextFile_AllLines(t *testing.T) {
	path := writeTempFile(t, "line1\nline2\nline3\n")
	lines, err := readTextFile(path, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"line1", "line2", "line3"}, lines)
}

func TestReadTextFile_Offset(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\nd\ne\n")
	lines, err := readTextFile(path, 2, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "d", "e"}, lines)
}

func TestReadTextFile_Limit(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\nd\ne\n")
	lines, err := readTextFile(path, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, lines)
}

func TestReadTextFile_OffsetAndLimit(t *testing.T) {
	path := writeTempFile(t, "a\nb\nc\nd\ne\n")
	lines, err := readTextFile(path, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "c"}, lines)
}

func TestReadTextFile_OffsetBeyondEnd(t *testing.T) {
	path := writeTempFile(t, "a\nb\n")
	lines, err := readTextFile(path, 100, 10)
	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestReadTextFile_LongLineTruncated(t *testing.T) {
	long := strings.Repeat("x", maxLineChars+100)
	path := writeTempFile(t, long+"\n")
	lines, err := readTextFile(path, 0, 10)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "[line truncated]")
	assert.LessOrEqual(t, len([]rune(lines[0])), maxLineChars+20)
}

func TestAddLineNumbers_StartsAtOne(t *testing.T) {
	result := addLineNumbers([]string{"foo", "bar"}, 1)
	assert.Contains(t, result, "     1\tfoo\n")
	assert.Contains(t, result, "     2\tbar\n")
}

func TestAddLineNumbers_OffsetStart(t *testing.T) {
	result := addLineNumbers([]string{"x"}, 51)
	assert.Contains(t, result, "    51\tx\n")
}

func TestReadFile_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	// Write maxFileBytes+1 bytes
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(maxFileBytes+1))
	require.NoError(t, f.Close())

	_, err = ReadFile(path, 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestReadFile_Directory(t *testing.T) {
	_, err := ReadFile(t.TempDir(), 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestReadFile_LineNumbers(t *testing.T) {
	path := writeTempFile(t, "hello\nworld\n")
	result, err := ReadFile(path, 0, 10)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "     1\thello")
	assert.Contains(t, result.Content, "     2\tworld")
}
