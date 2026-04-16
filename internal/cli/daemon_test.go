package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDaemonFlag_LongForm(t *testing.T) {
	orig := cgroupv2MemoryLimitMB
	cgroupv2MemoryLimitMB = 0
	defer func() { cgroupv2MemoryLimitMB = orig }()

	cmd := new(cobra.Command)
	cmd.Flags().IntVarP(&cgroupv2MemoryLimitMB, "cgroupv2-memory-limit", "m", 0, "")
	if err := cmd.ParseFlags([]string{"--cgroupv2-memory-limit", "128"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cgroupv2MemoryLimitMB != 128 {
		t.Errorf("cgroupv2MemoryLimitMB = %d, want 128", cgroupv2MemoryLimitMB)
	}
}

func TestDaemonFlag_ShortForm(t *testing.T) {
	orig := cgroupv2MemoryLimitMB
	cgroupv2MemoryLimitMB = 0
	defer func() { cgroupv2MemoryLimitMB = orig }()

	cmd := new(cobra.Command)
	cmd.Flags().IntVarP(&cgroupv2MemoryLimitMB, "cgroupv2-memory-limit", "m", 0, "")
	if err := cmd.ParseFlags([]string{"-m", "256"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cgroupv2MemoryLimitMB != 256 {
		t.Errorf("cgroupv2MemoryLimitMB = %d, want 256", cgroupv2MemoryLimitMB)
	}
}

func TestDaemonFlag_Default(t *testing.T) {
	orig := cgroupv2MemoryLimitMB
	cgroupv2MemoryLimitMB = 0
	defer func() { cgroupv2MemoryLimitMB = orig }()

	cmd := new(cobra.Command)
	cmd.Flags().IntVarP(&cgroupv2MemoryLimitMB, "cgroupv2-memory-limit", "m", 0, "")
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cgroupv2MemoryLimitMB != 0 {
		t.Errorf("cgroupv2MemoryLimitMB = %d, want 0 (default)", cgroupv2MemoryLimitMB)
	}
}

func TestDaemonFlag_NegativeAccepted(t *testing.T) {
	orig := cgroupv2MemoryLimitMB
	cgroupv2MemoryLimitMB = 0
	defer func() { cgroupv2MemoryLimitMB = orig }()

	cmd := new(cobra.Command)
	cmd.Flags().IntVarP(&cgroupv2MemoryLimitMB, "cgroupv2-memory-limit", "m", 0, "")
	// cobra does not reject negative values; daemon.Run interprets <=0 as "no limit".
	if err := cmd.ParseFlags([]string{"-m", "-1"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cgroupv2MemoryLimitMB != -1 {
		t.Errorf("cgroupv2MemoryLimitMB = %d, want -1 (cobra accepts negatives)", cgroupv2MemoryLimitMB)
	}
}
