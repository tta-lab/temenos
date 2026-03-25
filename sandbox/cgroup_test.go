//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestShortID(t *testing.T) {
	id, err := shortID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 16 {
		t.Errorf("expected 16 hex chars, got %d: %q", len(id), id)
	}
	id2, _ := shortID()
	if id == id2 {
		t.Error("two consecutive IDs should differ")
	}
}

func TestNewCgroupExec_Integration(t *testing.T) {
	if !cgroupAvailable() {
		t.Skip("cgroup v2 not available")
	}

	cg, err := newCgroupExec(128)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.cleanup()

	// Verify directory exists
	if _, err := os.Stat(cg.path); err != nil {
		t.Fatalf("cgroup dir should exist: %v", err)
	}

	// Verify memory.max
	data, err := os.ReadFile(filepath.Join(cg.path, "memory.max"))
	if err != nil {
		t.Fatal(err)
	}
	expected := strconv.FormatInt(128*1024*1024, 10)
	if got := string(data); got != expected {
		val, parseErr := strconv.ParseInt(got, 10, 64)
		if parseErr != nil || val != 128*1024*1024 {
			t.Errorf("memory.max = %q, want %s", got, expected)
		}
	}

	// Verify swap disabled
	swapData, err := os.ReadFile(filepath.Join(cg.path, "memory.swap.max"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(swapData); got != "0" {
		t.Errorf("memory.swap.max = %q, want 0", got)
	}

	// Cleanup removes dir
	cg.cleanup()
	if _, err := os.Stat(cg.path); !os.IsNotExist(err) {
		t.Error("cgroup dir should be removed after cleanup")
	}
}
