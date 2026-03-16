// Package sandbox — tool path configuration.
//
// Security model: "system tools are trusted, user data is not."
//
// The sandbox allows read access to tool installation directories
// (Homebrew, Go, Cargo, system package managers) by default.
// The security boundary protects user data — $HOME, project
// directories, and secrets — not tool installations.
//
// See docs/sandbox-security-model.md for the full rationale.
package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const darwinOS = "darwin"

// ToolDir describes a directory tree that should be accessible inside
// the sandbox. BinDir is added to PATH; ReadDirs are granted read
// access; ExecDirs allow shared library loading (file-map-executable
// on macOS seatbelt).
type ToolDir struct {
	BinDir   string   // added to $PATH (empty = skip)
	ReadDirs []string // granted read access in sandbox policy
	ExecDirs []string // granted file-map-executable (seatbelt only)
}

// staticToolDirs returns platform-specific tool directories that are
// always at fixed, well-known paths.
func staticToolDirs() []ToolDir {
	if runtime.GOOS == darwinOS {
		return darwinToolDirs()
	}
	return linuxToolDirs()
}

func darwinToolDirs() []ToolDir {
	return []ToolDir{
		// Apple Silicon Homebrew.
		{
			BinDir:   "/opt/homebrew/bin",
			ReadDirs: []string{"/opt/homebrew"},
			ExecDirs: []string{"/opt/homebrew"},
		},
		// Intel Homebrew / /usr/local tools.
		{
			BinDir:   "/usr/local/bin",
			ReadDirs: []string{"/usr/local"},
			ExecDirs: []string{"/usr/local/lib", "/usr/local/Cellar"},
		},
	}
}

func linuxToolDirs() []ToolDir {
	dirs := []ToolDir{
		// Linuxbrew / Homebrew on Linux.
		{
			BinDir:   "/home/linuxbrew/.linuxbrew/bin",
			ReadDirs: []string{"/home/linuxbrew/.linuxbrew"},
		},
		// Snap packages.
		{
			BinDir:   "/snap/bin",
			ReadDirs: []string{"/snap"},
		},
	}
	return dirs
}

// dynamicToolDirs returns tool directories resolved from the daemon's
// environment (GOPATH, HOME). These vary per installation.
func dynamicToolDirs() []ToolDir {
	var dirs []ToolDir

	// Go: GOPATH/bin (typically ~/go/bin).
	if gopathBin := resolveGOPATHBin(); gopathBin != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   gopathBin,
			ReadDirs: []string{gopathBin},
		})
	}

	// Cargo: ~/.cargo/bin.
	if cargoBin := resolveHomeSub(".cargo", "bin"); cargoBin != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   cargoBin,
			ReadDirs: []string{cargoBin},
		})
	}

	return dirs
}

// resolveGOPATHBin returns the GOPATH/bin directory, or empty if unavailable.
func resolveGOPATHBin() string {
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(gopath, "bin")
	}
	if h := daemonHome(); h != "" {
		return filepath.Join(h, "go", "bin")
	}
	return ""
}

// resolveHomeSub returns $HOME/<parts...> if HOME is available, else empty.
func resolveHomeSub(parts ...string) string {
	h := daemonHome()
	if h == "" {
		return ""
	}
	elems := append([]string{h}, parts...)
	return filepath.Join(elems...)
}

// daemonHome returns the daemon process's home directory.
func daemonHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// allToolDirs returns static + dynamic tool dirs, filtered to those
// whose BinDir actually exists on disk.
func allToolDirs() []ToolDir {
	all := append(staticToolDirs(), dynamicToolDirs()...)
	var result []ToolDir
	for _, td := range all {
		if td.BinDir == "" {
			continue
		}
		if _, err := os.Stat(td.BinDir); err != nil {
			continue
		}
		result = append(result, td)
	}
	return result
}

// buildSandboxPATH constructs the PATH string for sandboxed processes.
// It starts with the base system dirs (/usr/bin, /bin) then appends
// all discovered tool bin dirs.
func buildSandboxPATH() string {
	tools := allToolDirs()
	base := make([]string, 0, 2+len(tools))
	base = append(base, "/usr/bin", "/bin")
	for _, td := range tools {
		base = append(base, td.BinDir)
	}
	return strings.Join(base, ":")
}

// seatbeltMetadataDirs returns parent directories that need
// file-read-metadata for symlink resolution (e.g. /opt for
// /opt/homebrew, needed by Node's realpathSync).
func seatbeltMetadataDirs() []string {
	if runtime.GOOS == darwinOS {
		return []string{"/opt"}
	}
	return nil
}
