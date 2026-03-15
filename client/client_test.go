package client

import (
	"testing"
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
			name:    "https URL",
			addr:    "https://temenos.svc:8081",
			baseURL: "https://temenos.svc:8081",
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
}
