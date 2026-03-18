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
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
		// Intel Homebrew / /usr/local: /usr/local/bin is in the base PATH
		// unconditionally; read and exec grants are static in seatbelt_platform.sbpl.
		// No ToolDir entry needed here.
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

	// mise: ~/.local/share/mise (polyglot version manager — node, python, ruby, etc.).
	// Shims dir for PATH, full installs tree for read access.
	if miseRoot := resolveHomeSub(".local", "share", "mise"); miseRoot != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   filepath.Join(miseRoot, "shims"),
			ReadDirs: []string{miseRoot},
		})
	}

	// ~/.local/bin: standard user bin dir (pipx, user-installed scripts).
	if localBin := resolveHomeSub(".local", "bin"); localBin != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   localBin,
			ReadDirs: []string{localBin},
		})
	}

	// Bun: ~/.bun/bin.
	if bunBin := resolveHomeSub(".bun", "bin"); bunBin != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   bunBin,
			ReadDirs: []string{bunBin},
		})
	}

	// proto: ~/.proto/bin (toolchain manager).
	if protoBin := resolveHomeSub(".proto", "bin"); protoBin != "" {
		dirs = append(dirs, ToolDir{
			BinDir:   protoBin,
			ReadDirs: []string{protoBin},
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
	h, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("sandbox: HOME unset and UserHomeDir failed — dynamic tool dirs will be absent",
			"err", err)
		return ""
	}
	return h
}

var (
	cachedToolDirs     []ToolDir
	cachedToolDirsOnce sync.Once
)

// allToolDirs returns static + dynamic tool dirs, filtered to those
// whose BinDir actually exists on disk. Results are cached after first call
// since HOME/GOPATH are stable for the daemon's lifetime.
func allToolDirs() []ToolDir {
	cachedToolDirsOnce.Do(func() {
		static := staticToolDirs()
		dynamic := dynamicToolDirs()
		all := make([]ToolDir, 0, len(static)+len(dynamic))
		all = append(all, static...)
		all = append(all, dynamic...)

		for _, td := range all {
			if td.BinDir == "" {
				continue
			}
			if _, err := os.Stat(td.BinDir); err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					slog.Warn("sandbox: unexpected error checking tool dir",
						"path", td.BinDir, "err", err)
				}
				continue
			}
			cachedToolDirs = append(cachedToolDirs, td)
		}
	})
	return cachedToolDirs
}

// resetToolDirsCache clears the cached tool dirs (for testing only).
// NOT parallel-safe — callers must not use t.Parallel().
func resetToolDirsCache() {
	cachedToolDirsOnce = sync.Once{}
	cachedToolDirs = nil
}

// buildSandboxPATH constructs the PATH string for sandboxed processes.
// It starts with the base system dirs (/usr/bin, /usr/local/bin, /bin) then
// appends all discovered tool bin dirs. /usr/local/bin is included
// unconditionally: it is the standard FHS location for locally installed
// binaries on both Linux and macOS.
//
// Policy note: on macOS (seatbelt), read and exec access for /usr/local is
// granted statically in seatbelt_platform.sbpl — independent of whether
// /usr/local/bin exists on disk. On Linux (bwrap), /usr is already mounted
// read-only via --ro-bind /usr /usr, so no extra grant is needed.
func buildSandboxPATH() string {
	tools := allToolDirs()
	base := make([]string, 0, 3+len(tools))
	base = append(base, "/usr/bin", "/usr/local/bin", "/bin")
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
