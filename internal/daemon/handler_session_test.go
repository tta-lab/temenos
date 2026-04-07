package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/internal/session"
)

func makeSessionStore(t *testing.T) *session.Store {
	return session.NewStore(t.TempDir() + "/sessions.json")
}

// --- handleHTTPSessionRegister ---

func TestHandleHTTPSessionRegister_ValidRequest(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	body, _ := json.Marshal(session.RegisterRequest{Agent: "test-agent", WritePaths: []string{"/tmp/write"}})
	req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp SessionRegisterResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Token, 64, "token should be 64 hex chars")
	assert.Regexp(t, `[a-f0-9]{64}`, resp.Token)
}

func TestHandleHTTPSessionRegister_MissingAgent(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	body, _ := json.Marshal(session.RegisterRequest{Agent: ""})
	req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errResp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "agent")
}

func TestHandleHTTPSessionRegister_InvalidPaths(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	tests := []struct {
		name string
		req  session.RegisterRequest
	}{
		{"relative write path", session.RegisterRequest{Agent: "a", WritePaths: []string{"relative/path"}}},
		{"empty read path", session.RegisterRequest{Agent: "a", ReadPaths: []string{""}}},
		{"overlapping paths", session.RegisterRequest{
			Agent: "a", WritePaths: []string{"/shared"}, ReadPaths: []string{"/shared"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.req)
			req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

// --- handleHTTPSessionDelete ---

func registerSession(t *testing.T, store *session.Store, agent string) string {
	s, err := store.Register(session.RegisterRequest{Agent: agent})
	require.NoError(t, err)
	return s.Token
}

func TestHandleHTTPSessionDelete_KnownToken(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionDelete(store)

	token := registerSession(t, store, "test-agent")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req := httptest.NewRequest(http.MethodDelete, "/session/"+token, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestHandleHTTPSessionDelete_UnknownToken(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionDelete(store)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", "unknown-token")
	req := httptest.NewRequest(http.MethodDelete, "/session/unknown-token", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleHTTPSessionDelete_AlreadyDeletedToken(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionDelete(store)

	token := registerSession(t, store, "test-agent")

	// Delete once
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req := httptest.NewRequest(http.MethodDelete, "/session/"+token, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// Delete again — should still be 404 (idempotent at HTTP level)
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("token", token)
	req2 := httptest.NewRequest(http.MethodDelete, "/session/"+token, nil)
	req2 = req2.WithContext(context.WithValue(req2.Context(), chi.RouteCtxKey, rctx2))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusNotFound, rec2.Code)
}

// --- handleHTTPSessionList ---

func TestHandleHTTPSessionList_EmptyStore(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionList(store)

	req := httptest.NewRequest(http.MethodGet, "/session/list", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var sessions []session.Session
	err := json.NewDecoder(rec.Body).Decode(&sessions)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func TestHandleHTTPSessionList_AfterRegister(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionList(store)

	registerSession(t, store, "agent-1")

	req := httptest.NewRequest(http.MethodGet, "/session/list", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var sessions []session.Session
	err := json.NewDecoder(rec.Body).Decode(&sessions)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "agent-1", sessions[0].Agent)
}

// --- Full round trip ---

func TestHandleHTTPSession_RoundTrip(t *testing.T) {
	store := makeSessionStore(t)
	registerH := handleHTTPSessionRegister(store)
	deleteH := handleHTTPSessionDelete(store)
	listH := handleHTTPSessionList(store)

	// Register
	body, _ := json.Marshal(session.RegisterRequest{Agent: "roundtrip-agent"})
	regReq := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	registerH.ServeHTTP(regRec, regReq)
	require.Equal(t, http.StatusOK, regRec.Code)

	var regResp SessionRegisterResponse
	err := json.NewDecoder(regRec.Body).Decode(&regResp)
	require.NoError(t, err)
	token := regResp.Token

	// GET list — verify token present
	listReq := httptest.NewRequest(http.MethodGet, "/session/list", nil)
	listRec := httptest.NewRecorder()
	listH.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code)

	var sessions []session.Session
	err = json.NewDecoder(listRec.Body).Decode(&sessions)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, token, sessions[0].Token)

	// DELETE session
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	delReq := httptest.NewRequest(http.MethodDelete, "/session/"+token, nil)
	delReq = delReq.WithContext(context.WithValue(delReq.Context(), chi.RouteCtxKey, rctx))
	delRec := httptest.NewRecorder()
	deleteH.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code)

	// GET list again — verify empty
	listReq2 := httptest.NewRequest(http.MethodGet, "/session/list", nil)
	listRec2 := httptest.NewRecorder()
	listH.ServeHTTP(listRec2, listReq2)
	require.Equal(t, http.StatusOK, listRec2.Code)

	var sessions2 []session.Session
	err = json.NewDecoder(listRec2.Body).Decode(&sessions2)
	require.NoError(t, err)
	assert.Empty(t, sessions2)
}

// TestHTTPSessionRegister_WithEnv verifies that registering with Env persists it.
func TestHTTPSessionRegister_WithEnv(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	body, _ := json.Marshal(session.RegisterRequest{
		Agent:      "env-agent",
		WritePaths: []string{"/tmp/write"},
		Env:        map[string]string{"FOO": "bar", "BAZ": "qux"},
	})
	req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp SessionRegisterResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Token, 64)

	// Verify Env persisted via store
	sess := store.Get(resp.Token)
	require.NotNil(t, sess)
	assert.Equal(t, "bar", sess.Env["FOO"])
	assert.Equal(t, "qux", sess.Env["BAZ"])
}

// TestHTTPSessionRegister_InvalidEnvKey verifies that invalid Env keys return 400.
func TestHTTPSessionRegister_InvalidEnvKey(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	invalidEnvs := []map[string]string{
		{"=bad": "x"},
		{"1BAD": "x"},
		{"FOO BAR": "x"},
		{"": "x"},
	}

	for _, env := range invalidEnvs {
		body, _ := json.Marshal(session.RegisterRequest{Agent: "a", Env: env})
		req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "expected 400 for env %v", env)
	}
}

// TestHTTPSessionRegister_InvalidEnvValue verifies that invalid Env values return 400.
func TestHTTPSessionRegister_InvalidEnvValue(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	invalidValues := []map[string]string{
		{"FOO": "bar\nbaz"},
		{"FOO": "bar\rbaz"},
		{"FOO": "bar\x00baz"},
	}

	for _, env := range invalidValues {
		body, _ := json.Marshal(session.RegisterRequest{Agent: "a", Env: env})
		req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, "expected 400 for env value %v", env)
	}
}

// TestHTTPSessionRegister_ValidEnvValues verifies that valid Env values are accepted.
func TestHTTPSessionRegister_ValidEnvValues(t *testing.T) {
	store := makeSessionStore(t)
	h := handleHTTPSessionRegister(store)

	validValues := []map[string]string{
		{"FOO": ""},
		{"FOO": "a=b=c"},
		{"FOO": "$HOME"},
		{"FOO": "${VAR}"},
		{"FOO": "`cmd`"},
	}

	for _, env := range validValues {
		body, _ := json.Marshal(session.RegisterRequest{Agent: "a", Env: env})
		req := httptest.NewRequest(http.MethodPost, "/session/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "expected 200 for env value %v", env)
	}
}

// TestHTTPSessionList_IncludesEnv verifies that List() includes Env in the response.
func TestHTTPSessionList_IncludesEnv(t *testing.T) {
	store := makeSessionStore(t)

	_, err := store.Register(session.RegisterRequest{
		Agent: "env-agent",
		Env:   map[string]string{"FOO": "bar"},
	})
	require.NoError(t, err)

	h := handleHTTPSessionList(store)
	req := httptest.NewRequest(http.MethodGet, "/session/list", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var sessions []session.Session
	err = json.NewDecoder(rec.Body).Decode(&sessions)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "bar", sessions[0].Env["FOO"])
}
