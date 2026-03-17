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
		// Should include Homebrew entries.
		binDirs := make([]string, 0, len(dirs))
		for _, td := range dirs {
			binDirs = append(binDirs, td.BinDir)
		}
		assert.Contains(t, binDirs, "/opt/homebrew/bin")
		assert.Contains(t, binDirs, "/usr/local/bin")
	}
}

func TestBuildSandboxPATH_ContainsBase(t *testing.T) {
	path := buildSandboxPATH()
	assert.True(t, strings.HasPrefix(path, "/usr/bin:/bin"),
		"PATH should start with /usr/bin:/bin, got: %s", path)
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

func TestDynamicToolDirs_IncludesGOPATH(t *testing.T) {
	t.Setenv("GOPATH", "/test/gopath")
	dirs := dynamicToolDirs()
	binDirs := make([]string, 0, len(dirs))
	for _, td := range dirs {
		binDirs = append(binDirs, td.BinDir)
	}
	assert.Contains(t, binDirs, "/test/gopath/bin")
}

func TestDynamicToolDirs_IncludesAllExpected(t *testing.T) {
	t.Setenv("HOME", "/test/home")
	dirs := dynamicToolDirs()
	binDirs := make([]string, 0, len(dirs))
	for _, td := range dirs {
		binDirs = append(binDirs, td.BinDir)
	}
	assert.Contains(t, binDirs, "/test/home/.cargo/bin")
	assert.Contains(t, binDirs, "/test/home/.local/share/mise/shims")
	assert.Contains(t, binDirs, "/test/home/.local/bin")
	assert.Contains(t, binDirs, "/test/home/.bun/bin")
	assert.Contains(t, binDirs, "/test/home/.proto/bin")
}

func TestDynamicToolDirs_MiseReadDirsCoversInstalls(t *testing.T) {
	t.Setenv("HOME", "/test/home")
	dirs := dynamicToolDirs()
	for _, td := range dirs {
		if td.BinDir == "/test/home/.local/share/mise/shims" {
			assert.Contains(t, td.ReadDirs, "/test/home/.local/share/mise",
				"mise ReadDirs should cover the full installs tree")
			return
		}
	}
	t.Fatal("mise entry not found in dynamicToolDirs")
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
