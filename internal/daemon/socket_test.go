package daemon

import (
	"testing"
)

func TestParseListenAddr(t *testing.T) {
	tests := []struct {
		addr    string
		network string
		listen  string
	}{
		{"/tmp/temenos.sock", "unix", "/tmp/temenos.sock"},
		{"./temenos.sock", "unix", "./temenos.sock"},
		{":8081", "tcp", ":8081"},
		{"0.0.0.0:8081", "tcp", "0.0.0.0:8081"},
		{"localhost:8081", "tcp", "localhost:8081"},
	}
	for _, tt := range tests {
		network, listen := parseListenAddr(tt.addr)
		if network != tt.network || listen != tt.listen {
			t.Errorf("parseListenAddr(%q) = %q, %q; want %q, %q",
				tt.addr, network, listen, tt.network, tt.listen)
		}
	}
}
