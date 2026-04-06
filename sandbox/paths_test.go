package sandbox

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGOPATHBin_ExplicitGOPATH(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")
	assert.Equal(t, "/test/gopath/bin", resolveGOPATHBin())
}

func TestResolveGOPATHBin_FallbackToHome(t *testing.T) {
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "/test/home")
	assert.Equal(t, "/test/home/go/bin", resolveGOPATHBin())
}

func TestResolveHomeSub(t *testing.T) {
	t.Setenv("HOME", "/test/home")
	assert.Equal(t, "/test/home/.cargo/bin", resolveHomeSub(".cargo", "bin"))
}

func TestResolveHomeSub_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	// Skip if UserHomeDir succeeds via syscall (would give a non-empty result).
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("UserHomeDir succeeds without HOME — cannot test empty fallback")
	}
	assert.Equal(t, "", resolveHomeSub(".cargo", "bin"))
}

func TestStaticToolDirs_ReturnsForPlatform(t *testing.T) {
	dirs := staticToolDirs()
	require.NotEmpty(t, dirs)

	if runtime.GOOS == darwinOS {
		// Should include Apple Silicon Homebrew.
		// /usr/local/bin is not a ToolDir BinDir — it is in the base PATH
		// unconditionally and its seatbelt grants live in seatbelt_platform.sbpl.
		binDirs := make([]string, 0, len(dirs))
		for _, td := range dirs {
			binDirs = append(binDirs, td.BinDir)
		}
		assert.Contains(t, binDirs, "/opt/homebrew/bin")
		// /usr/local/bin must not be a ToolDir BinDir — it comes from the base PATH.
		assert.NotContains(t, binDirs, "/usr/local/bin")
	}
}

func TestBuildSandboxPATH_ContainsBase(t *testing.T) {
	path := buildSandboxPATH()
	assert.True(t, strings.HasPrefix(path, "/usr/bin:/usr/local/bin:/bin"),
		"PATH should start with /usr/bin:/usr/local/bin:/bin, got: %s", path)
	assert.Equal(t, 1, strings.Count(path, "/usr/local/bin"),
		"/usr/local/bin should appear exactly once in PATH, got: %s", path)
}

func TestBuildSandboxPATH_ContainsGOPATH(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")
	// Only works if /test/gopath/bin exists — skip if it doesn't.
	if _, err := os.Stat("/test/gopath/bin"); err != nil {
		t.Skip("/test/gopath/bin does not exist")
	}
	path := buildSandboxPATH()
	assert.Contains(t, path, "/test/gopath/bin")
}

func TestDynamicToolDirs_ReturnsAllExpectedDirectories(t *testing.T) {
	t.Setenv("HOME", "/test/home")
	t.Setenv("GOPATH", "/test/gopath")

	dirs := dynamicToolDirs()
	binDirMap := make(map[string]ToolDir)
	for _, td := range dirs {
		binDirMap[td.BinDir] = td
	}

	tests := []struct {
		name     string
		binDir   string
		readDirs []string
	}{
		{"GOPATH bin", "/test/gopath/bin", []string{"/test/gopath/bin"}},
		{".cargo/bin", "/test/home/.cargo/bin", []string{"/test/home/.cargo/bin"}},
		{".local/share/mise/shims", "/test/home/.local/share/mise/shims", []string{"/test/home/.local/share/mise"}},
		{".local/bin", "/test/home/.local/bin", []string{"/test/home/.local/bin"}},
		{".bun/bin", "/test/home/.bun/bin", []string{"/test/home/.bun/bin"}},
		{".proto/bin", "/test/home/.proto/bin", []string{"/test/home/.proto/bin"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td, ok := binDirMap[tt.binDir]
			assert.True(t, ok, "expected %s in dirs", tt.binDir)
			assert.Equal(t, tt.readDirs, td.ReadDirs)
		})
	}
}

func TestAllToolDirs_FiltersNonExistent(t *testing.T) {
	t.Setenv("GOPATH", "/nonexistent/gopath")
	t.Setenv("HOME", "/nonexistent/home")
	resetToolDirsCache()
	t.Cleanup(resetToolDirsCache)
	dirs := allToolDirs()
	for _, td := range dirs {
		_, err := os.Stat(td.BinDir)
		assert.NoError(t, err, "allToolDirs should only return existing dirs, got: %s", td.BinDir)
	}
}

func TestSeatbeltMetadataDirs(t *testing.T) {
	dirs := seatbeltMetadataDirs()
	if runtime.GOOS == darwinOS {
		assert.Contains(t, dirs, "/opt")
	} else {
		assert.Empty(t, dirs)
	}
}

func TestIsSubdirOf(t *testing.T) {
	assert.True(t, isSubdirOf("/usr/local/bin", "/usr"))
	assert.False(t, isSubdirOf("/opt/homebrew", "/usr"))
	assert.False(t, isSubdirOf("/usr", "/usr"))
}
