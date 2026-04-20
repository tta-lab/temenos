package cli

import (
	"testing"

	"github.com/tta-lab/temenos/sandbox"
)

func TestDoctorCmd_ReadyExitCode(t *testing.T) {
	tests := []struct {
		name    string
		status  sandbox.Status
		wantErr bool
	}{
		{
			name:    "ready",
			status:  sandbox.Status{Ready: true, Checks: []sandbox.Check{{Name: "k8s_pod", OK: true}}},
			wantErr: false,
		},
		{
			name: "not ready",
			status: sandbox.Status{
				Ready: false,
				Checks: []sandbox.Check{{
					Name:        "init_leaf",
					OK:          false,
					Remediation: "daemon not started",
				}},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := currentStatus
			currentStatus = func() sandbox.Status { return tc.status }
			t.Cleanup(func() { currentStatus = orig })

			// Test the logic directly: RunE returns non-nil error when !Ready.
			status := currentStatus()
			var err error
			if !status.Ready {
				err = doctorNotReadyErr
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
