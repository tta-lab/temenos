package daemon

import (
	"fmt"
	"os"
	"strconv"
)

const maxMemoryLimitMB = 32768 // 32 GB — sane ceiling

// parseRequireCgroup reads TEMENOS_REQUIRE_CGROUP from the environment.
// Returns true only when the value is exactly "true".
func parseRequireCgroup() bool {
	return os.Getenv("TEMENOS_REQUIRE_CGROUP") == "true"
}

// parseMemoryLimitMB reads TEMENOS_MEMORY_LIMIT_MB from the environment.
// Returns 0 if unset. Returns an error for invalid values (non-integer, ≤ 0, or > 32 GB).
func parseMemoryLimitMB() (int, error) {
	v := os.Getenv("TEMENOS_MEMORY_LIMIT_MB")
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("TEMENOS_MEMORY_LIMIT_MB=%q: %w", v, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("TEMENOS_MEMORY_LIMIT_MB must be positive, got %d", n)
	}
	if n > maxMemoryLimitMB {
		return 0, fmt.Errorf("TEMENOS_MEMORY_LIMIT_MB=%d exceeds maximum %d", n, maxMemoryLimitMB)
	}
	return int(n), nil
}
