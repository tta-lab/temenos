package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    string
		wantErr error
	}{
		{name: "valid token", header: "Bearer mytoken", want: "mytoken", wantErr: nil},
		{name: "missing header", header: "", want: "", wantErr: ErrTokenMissing},
		{name: "wrong prefix", header: "Basic mytoken", want: "", wantErr: ErrInvalidAuthHeader},
		{name: "empty token", header: "Bearer ", want: "", wantErr: ErrTokenMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got, err := extractBearerToken(req)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

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
			assert.Equal(t, tt.want, isRequiredSA(tt.user, tt.allowed))
		})
	}
}

func TestValidateSAJWTMiddleware_NoToken_Returns401(t *testing.T) {
	mw := ValidateSAJWTMiddleware([]string{"sa:ns:name"}, "https://example.com")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/run", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	assert.Equal(t, "authorization required", body["error"])
}

func TestValidateSAJWTMiddleware_MalformedToken_Returns401(t *testing.T) {
	mw := ValidateSAJWTMiddleware([]string{"sa:ns:name"}, "https://example.com")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/run", nil)
	req.Header.Set("Authorization", "Basic xyz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
