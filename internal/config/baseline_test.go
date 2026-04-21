package config

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBaselineAllowEnv_ExcludesOverridableKeys is a regression guard: PATH
// and TERM are deliberately injected by sandbox.buildEnv and must NEVER be
// in the baseline (would let callers override the sandbox PATH/TERM via
// duplicate-key precedence in os/exec).
func TestBaselineAllowEnv_ExcludesOverridableKeys(t *testing.T) {
	for _, forbidden := range []string{"PATH", "TERM"} {
		assert.False(t, slices.Contains(BaselineAllowEnv, forbidden),
			"%s must NOT be in BaselineAllowEnv (see baseline.go exclusion rationale)", forbidden)
	}
}

// TestBaselineAllowEnv_PatternsValid ensures every baseline pattern is a
// valid filepath.Match pattern (Load's validateAllowEnv would reject it
// otherwise).
func TestBaselineAllowEnv_PatternsValid(t *testing.T) {
	for _, p := range BaselineAllowEnv {
		_, err := filepath.Match(p, "")
		assert.NoErrorf(t, err, "baseline pattern %q must be valid", p)
	}
}

// TestEffectiveAllowEnv_BaselineOnly: empty user list -> baseline only.
func TestEffectiveAllowEnv_BaselineOnly(t *testing.T) {
	cfg := &Config{}
	got := cfg.EffectiveAllowEnv()
	assert.Equal(t, BaselineAllowEnv, got)
}

// TestEffectiveAllowEnv_UserExtendsBaseline: user additions appended after
// baseline, original order preserved.
func TestEffectiveAllowEnv_UserExtendsBaseline(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"TTAL_*", "MY_VAR"}}
	got := cfg.EffectiveAllowEnv()
	assert.Equal(t, len(BaselineAllowEnv)+2, len(got))
	assert.Equal(t, "USER", got[0], "BaselineAllowEnv must come first")
	assert.Equal(t, "TTAL_*", got[len(BaselineAllowEnv)])
	assert.Equal(t, "MY_VAR", got[len(BaselineAllowEnv)+1])
}

// TestEffectiveAllowEnv_DedupesUserBaselineOverlap: a user pattern that
// duplicates a baseline pattern appears only once.
func TestEffectiveAllowEnv_DedupesUserBaselineOverlap(t *testing.T) {
	cfg := &Config{AllowEnv: []string{"LANG", "LC_*", "MY_VAR"}}
	got := cfg.EffectiveAllowEnv()
	assert.Equal(t, len(BaselineAllowEnv)+1, len(got), "only MY_VAR should add to baseline length")
	count := 0
	for _, p := range got {
		if p == "LANG" {
			count++
		}
	}
	assert.Equal(t, 1, count, "LANG should appear once")

	// Also verify LC_* appears exactly once (glob deduped).
	lcCount := 0
	for _, p := range got {
		if p == "LC_*" {
			lcCount++
		}
	}
	assert.Equal(t, 1, lcCount, "LC_* should appear once")
}

// TestFilterEnv_BaselinePassesWithoutUserConfig: empty user allow_env still
// forwards baseline keys; non-baseline keys are stripped.
func TestFilterEnv_BaselinePassesWithoutUserConfig(t *testing.T) {
	cfg := &Config{}
	env := map[string]string{
		"USER":         "alice",
		"LANG":         "en_US.UTF-8",
		"LC_ALL":       "C",
		"HOME":         "/home/alice",
		"GITHUB_TOKEN": "ghp_secret",
		"MY_VAR":       "x",
	}
	allowed, stripped := cfg.FilterEnv(env)
	assert.Equal(t, "alice", allowed["USER"])
	assert.Equal(t, "en_US.UTF-8", allowed["LANG"])
	assert.Equal(t, "C", allowed["LC_ALL"])
	assert.Equal(t, "/home/alice", allowed["HOME"])
	assert.NotContains(t, allowed, "GITHUB_TOKEN")
	assert.NotContains(t, allowed, "MY_VAR")
	assert.Equal(t, []string{"GITHUB_TOKEN", "MY_VAR"}, stripped)
}

// TestBaselineAllowEnv_IncludesTmuxSessionKeys is a positive-membership
// guard: TMUX and TMUX_PANE are deliberately in the baseline so ttal CLI
// inside worker sandboxes can prefix alerts with the tmux session name,
// ping counterpart agents on comment add, auto-close reviewer windows on
// LGTM, and attribute pipeline advances. Removing either key silently
// degrades these ops paths — see baseline.go rationale block.
func TestBaselineAllowEnv_IncludesTmuxSessionKeys(t *testing.T) {
	for _, required := range []string{"TMUX", "TMUX_PANE"} {
		assert.Truef(t, slices.Contains(BaselineAllowEnv, required),
			"%s must be in BaselineAllowEnv (see baseline.go rationale)", required)
	}
}

// TestFilterEnv_TmuxKeysPassBaseline: TMUX and TMUX_PANE forward through
// the sandbox with empty operator allow_env, same as USER/HOME. End-to-end
// check complementing the membership test above.
func TestFilterEnv_TmuxKeysPassBaseline(t *testing.T) {
	cfg := &Config{}
	env := map[string]string{
		"TMUX":        "/private/tmp/tmux-501/default,12345,3",
		"TMUX_PANE":   "%17",
		"TMUX_WINDOW": "1",
		"UNRELATED":   "stripped",
	}
	allowed, stripped := cfg.FilterEnv(env)
	assert.Equal(t, "/private/tmp/tmux-501/default,12345,3", allowed["TMUX"])
	assert.Equal(t, "%17", allowed["TMUX_PANE"])
	assert.NotContains(t, allowed, "TMUX_WINDOW")
	assert.NotContains(t, allowed, "UNRELATED")
	assert.ElementsMatch(t, []string{"TMUX_WINDOW", "UNRELATED"}, stripped)
}
