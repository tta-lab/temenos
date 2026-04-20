package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tta-lab/temenos/sandbox"
)

func TestDoctorCmd(t *testing.T) {
	readyStatus := sandbox.Status{
		Ready: true,
		Checks: []sandbox.Check{
			{Name: "k8s_pod", OK: true, Detail: "KUBERNETES_SERVICE_HOST set"},
		},
	}
	notReadyStatus := sandbox.Status{
		Ready: false,
		Checks: []sandbox.Check{
			{Name: "init_leaf", OK: false},
		},
	}

	tests := []struct {
		name    string
		status  sandbox.Status
		json    bool
		wantErr bool
	}{
		{name: "ready_text", status: readyStatus, json: false, wantErr: false},
		{name: "ready_json", status: readyStatus, json: true, wantErr: false},
		{name: "not_ready_text", status: notReadyStatus, json: false, wantErr: true},
		{name: "not_ready_json", status: notReadyStatus, json: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := currentStatus
			currentStatus = func() sandbox.Status { return tc.status }
			t.Cleanup(func() { currentStatus = orig })

			cmd := &cobra.Command{
				Use:               "doctor",
				Short:             doctorCmd.Short,
				SilenceUsage:      true,
				SilenceErrors:     true,
				DisableAutoGenTag: true,
				RunE:              doctorCmd.RunE,
			}
			doctorCmd.Flags().VisitAll(func(f *pflag.Flag) {
				cmd.Flags().AddFlag(f)
			})

			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			if tc.json {
				cmd.SetArgs([]string{"--json"})
			} else {
				cmd.SetArgs([]string{})
			}
			err := cmd.Execute()
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v wantErr=%v; out=%s", err, tc.wantErr, buf.String())
			}
		})
	}
}
