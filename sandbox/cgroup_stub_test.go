//go:build !linux
// +build !linux

package sandbox

import (
	"encoding/json"
	"testing"
)

func TestStatusJSON(t *testing.T) {
	status := Status{
		Ready: false,
		Checks: []Check{{
			Name:        "platform",
			OK:          false,
			Detail:      "non-Linux",
			Remediation: "temenos cgroup v2 sandbox requires Linux",
		}},
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got["ready"] != false {
		t.Errorf("ready: got %v, want false", got["ready"])
	}
	checks, ok := got["checks"].([]any)
	if !ok {
		t.Fatal("checks: not an array")
	}
	if len(checks) != 1 {
		t.Errorf("len(checks) = %d, want 1", len(checks))
	}

	var roundTrip Status
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal into Status: %v", err)
	}
	if roundTrip.Ready != status.Ready {
		t.Errorf("Round-trip Ready: got %v, want %v", roundTrip.Ready, status.Ready)
	}
}

func TestStatusString_Platform(t *testing.T) {
	status := CurrentStatus()
	got := status.String()
	want := "temenos doctor: not available on this platform"
	if got != want {
		t.Errorf("Status.String() = %q, want %q", got, want)
	}
}
