package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientTransport(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "unix socket absolute path",
			addr:    "/tmp/foo.sock",
			baseURL: "http://temenos",
		},
		{
			name:    "unix socket relative path",
			addr:    "./temenos.sock",
			baseURL: "http://temenos",
		},
		{
			name:    "http URL",
			addr:    "http://localhost:8081",
			baseURL: "http://localhost:8081",
		},
		{
			name:    "https URL rejected",
			addr:    "https://temenos.svc:8081",
			wantErr: true,
		},
		{
			name:    "bare host:port",
			addr:    ":8081",
			baseURL: "http://:8081",
		},
		{
			name:    "localhost:port",
			addr:    "localhost:8081",
			baseURL: "http://localhost:8081",
		},
		{
			name:    "http URL with trailing slash stripped",
			addr:    "http://localhost:8081/",
			baseURL: "http://localhost:8081",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.baseURL != tt.baseURL {
				t.Errorf("baseURL = %q; want %q", c.baseURL, tt.baseURL)
			}
		})
	}
}

func TestResolveAddrFromEnv(t *testing.T) {
	t.Run("TEMENOS_LISTEN_ADDR takes priority", func(t *testing.T) {
		t.Setenv("TEMENOS_LISTEN_ADDR", ":8081")
		t.Setenv("TEMENOS_SOCKET_PATH", "/tmp/other.sock")

		addr, err := resolveAddr()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != ":8081" {
			t.Errorf("resolveAddr() = %q; want %q", addr, ":8081")
		}
	})

	t.Run("TEMENOS_SOCKET_PATH fallback", func(t *testing.T) {
		t.Setenv("TEMENOS_LISTEN_ADDR", "")
		t.Setenv("TEMENOS_SOCKET_PATH", "/tmp/custom.sock")

		addr, err := resolveAddr()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if addr != "/tmp/custom.sock" {
			t.Errorf("resolveAddr() = %q; want %q", addr, "/tmp/custom.sock")
		}
	})

	t.Run("default socket path fallback", func(t *testing.T) {
		t.Setenv("TEMENOS_LISTEN_ADDR", "")
		t.Setenv("TEMENOS_SOCKET_PATH", "")

		addr, err := resolveAddr()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(addr, ".temenos/daemon.sock") {
			t.Errorf("resolveAddr() = %q; want suffix .temenos/daemon.sock", addr)
		}
	})
}

func TestNewEmptyAddrResolvesViaEnv(t *testing.T) {
	t.Setenv("TEMENOS_LISTEN_ADDR", "/tmp/env-resolved.sock")
	t.Setenv("TEMENOS_SOCKET_PATH", "")

	c, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// env resolved to a unix path — must use unix transport (baseURL = http://temenos)
	if c.baseURL != "http://temenos" {
		t.Errorf("baseURL = %q; want http://temenos (unix transport)", c.baseURL)
	}
}

func TestClient_SetAuthToken(t *testing.T) {
	c := &Client{baseURL: "http://localhost", httpClient: &http.Client{}}

	c.SetAuthToken("my-jwt-token")
	assert.Equal(t, "my-jwt-token", c.authToken)
}

func TestClient_postsWithAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token-abc", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"stdout":"ok","stderr":"","exit_code":0}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, httpClient: srv.Client()}
	c.SetAuthToken("token-abc")

	_, err := c.Run(context.Background(), RunRequest{Command: "echo hi"})
	require.NoError(t, err)
}
