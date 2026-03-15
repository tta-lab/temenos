package tools

import (
	"fmt"
	"os"
)

// ReadFileResult holds the output of a file read operation.
type ReadFileResult struct {
	Content string // line-numbered content
	Lines   int    // number of lines returned
}

// ReadFile reads a file with line numbers, offset/limit, and safety guards.
// offset is 0-based. limit defaults to 2000 if ≤ 0.
// Returns an error if the file is too large (>5MB) or is a directory.
func ReadFile(path string, offset, limit int) (*ReadFileResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory, not a file", path)
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("file too large (%d bytes, max %d bytes)", info.Size(), maxFileBytes)
	}

	if limit <= 0 {
		limit = defaultReadLines
	}

	lines, err := readTextFile(path, offset, limit)
	if err != nil {
		return nil, err
	}

	return &ReadFileResult{
		Content: addLineNumbers(lines, offset+1),
		Lines:   len(lines),
	}, nil
}
