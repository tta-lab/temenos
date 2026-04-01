package sandbox

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed seatbelt_base.sbpl
var seatbeltBase string

//go:embed seatbelt_network.sbpl
var seatbeltNetwork string

//go:embed seatbelt_platform.sbpl
var seatbeltPlatform string

// buildPolicy assembles a seatbelt policy string from embedded templates and mount config.
// Returns the policy text and -D parameter flags for sandbox-exec.
// Returns an error if any Mount has Source != Target (seatbelt can't remap paths).
// buildPolicy assembles a seatbelt policy string from embedded templates and mount config.
// Returns the policy text and -D parameter flags for sandbox-exec.
// Returns an error if any non-MetadataOnly Mount has Source != Target (seatbelt can't remap paths).
func buildPolicy(cfg *ExecConfig) (policy string, params []string, err error) {
	var b strings.Builder
	b.WriteString(seatbeltBase)
	b.WriteString("\n")
	b.WriteString(seatbeltPlatform)
	b.WriteString("\n")
	b.WriteString(seatbeltNetwork)

	readableIdx := 0
	writableIdx := 0
	metadataIdx := 0

	if cfg != nil {
		for _, m := range cfg.MountDirs {
			if m.MetadataOnly {
				key := fmt.Sprintf("METADATA_ROOT_%d", metadataIdx)
				fmt.Fprintf(&b, "\n(allow file-read-metadata (literal (param %q)))", key)
				params = append(params, "-D", key+"="+m.Source)
				metadataIdx++
				continue
			}
			if m.Source != m.Target {
				return "", nil, fmt.Errorf(
					"seatbelt cannot remap paths: mount source %q != target %q",
					m.Source, m.Target,
				)
			}
			if m.ReadOnly {
				key := fmt.Sprintf("READABLE_ROOT_%d", readableIdx)
				fmt.Fprintf(&b, "\n(allow file-read* (subpath (param %q)))", key)
				params = append(params, "-D", key+"="+m.Source)
				readableIdx++
			} else {
				key := fmt.Sprintf("WRITABLE_ROOT_%d", writableIdx)
				fmt.Fprintf(&b, "\n(allow file-read* file-write* (subpath (param %q)))", key)
				params = append(params, "-D", key+"="+m.Source)
				writableIdx++
			}
		}
	}

	// Inject tool directory rules from the path registry (see paths.go).
	_, params = appendToolDirRules(&b, readableIdx, params)

	// Metadata dirs for symlink resolution (e.g. /opt for /opt/homebrew).
	for _, d := range seatbeltMetadataDirs() {
		fmt.Fprintf(&b, "\n(allow file-read-metadata (literal %q))", d)
	}

	// Add DARWIN_USER_CACHE_DIR for TLS cache.
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", nil, fmt.Errorf("resolve user cache dir: %w", err)
	}
	params = append(params, "-D", "DARWIN_USER_CACHE_DIR="+cacheDir)

	return b.String(), params, nil
}

// appendToolDirRules adds seatbelt read and executable rules for all
// discovered tool directories. Returns updated readableIdx and params.
func appendToolDirRules(b *strings.Builder, readableIdx int, params []string) (int, []string) {
	seen := make(map[string]bool) // deduplicate paths

	for _, td := range allToolDirs() {
		for _, rd := range td.ReadDirs {
			if seen[rd] {
				continue
			}
			seen[rd] = true
			key := fmt.Sprintf("READABLE_ROOT_%d", readableIdx)
			fmt.Fprintf(b, "\n(allow file-read* (subpath (param %q)))", key)
			params = append(params, "-D", key+"="+rd)
			readableIdx++
		}
		for _, ed := range td.ExecDirs {
			if seen["exec:"+ed] {
				continue
			}
			seen["exec:"+ed] = true
			fmt.Fprintf(b, "\n(allow file-map-executable (subpath %q))", ed)
		}
	}

	return readableIdx, params
}
