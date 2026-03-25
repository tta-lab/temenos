package daemon

import (
	"testing"
)

func TestParseMemoryLimitMB(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    int
		wantErr bool
	}{
		{"unset", "", 0, false},
		{"valid", "128", 128, false},
		{"valid large", "1024", 1024, false},
		{"non-integer", "abc", 0, true},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"ceiling", "32768", 32768, false},
		{"over ceiling", "32769", 0, true},
		{"exceeds max", "99999", 0, true},
		{"float", "128.5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEMENOS_MEMORY_LIMIT_MB", tt.env)

			got, err := parseMemoryLimitMB()
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemoryLimitMB() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseMemoryLimitMB() = %v, want %v", got, tt.want)
			}
		})
	}
}
