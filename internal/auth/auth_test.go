package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRequiredSA(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		allowed []string
		want    bool
	}{
		{name: "match", user: "sa:ns:name", allowed: []string{"sa:ns:name", "other:ns:name"}, want: true},
		{name: "no match", user: "sa:ns:other", allowed: []string{"sa:ns:name"}, want: false},
		{name: "empty allowlist", user: "sa:ns:name", allowed: nil, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsRequiredSA(tt.user, tt.allowed))
		})
	}
}
