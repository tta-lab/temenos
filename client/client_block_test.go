package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunBlock_RoundTrip(t *testing.T) {
	want := RunBlockResponse{
		Results: []CommandResult{
			{Command: "echo hello", Stdout: "hello\n", Stderr: "", ExitCode: 0},
			{Command: "pwd", Stdout: "/tmp\n", Stderr: "", ExitCode: 0},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/run-block" {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusNotFound)
			return
		}

		var req RunBlockRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Verify the request fields were encoded correctly.
		if req.Block == "" {
			http.Error(w, "block is empty", http.StatusBadRequest)
			return
		}
		if req.Prefix != "§ " {
			http.Error(w, "unexpected prefix: "+req.Prefix, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(want); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := c.RunBlock(context.Background(), RunBlockRequest{
		Block:  "§ echo hello\n§ pwd\n",
		Prefix: "§ ",
	})
	if err != nil {
		t.Fatalf("RunBlock: %v", err)
	}

	if len(resp.Results) != len(want.Results) {
		t.Fatalf("got %d results, want %d", len(resp.Results), len(want.Results))
	}
	for i, r := range resp.Results {
		w := want.Results[i]
		if r.Command != w.Command {
			t.Errorf("result[%d].Command = %q, want %q", i, r.Command, w.Command)
		}
		if r.Stdout != w.Stdout {
			t.Errorf("result[%d].Stdout = %q, want %q", i, r.Stdout, w.Stdout)
		}
		if r.ExitCode != w.ExitCode {
			t.Errorf("result[%d].ExitCode = %d, want %d", i, r.ExitCode, w.ExitCode)
		}
	}
}

func TestRunBlock_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "validation error", http.StatusBadRequest)
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = c.RunBlock(context.Background(), RunBlockRequest{
		Block:  "§ ls\n",
		Prefix: "§ ",
	})
	if err == nil {
		t.Fatal("expected error from non-200 status, got nil")
	}
}
