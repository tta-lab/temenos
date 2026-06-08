// Package auth provides authentication middleware for Temenos.
//
// In Kubernetes mode, Temenos can be configured to require a valid Kubernetes
// service account token (JWT) on every /run request. The token is validated
// against the Kubernetes TokenReview API.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// ErrTokenMissing is returned when no Authorization header is present.
var ErrTokenMissing = errors.New("authorization header missing")

// ErrInvalidAuthHeader is returned when the Authorization header format is invalid.
var ErrInvalidAuthHeader = errors.New("invalid authorization header format")

// ErrForbidden is returned when the token is invalid or the caller identity
// does not match the required service account.
var ErrForbidden = errors.New("access denied — invalid token or caller identity")

type tokenReviewRequest struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Token     string   `json:"token"`
		Audiences []string `json:"audiences,omitempty"`
	} `json:"spec"`
}

type tokenReviewResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Status     struct {
		Authenticated bool            `json:"authenticated"`
		Error         string          `json:"error,omitempty"`
		User          tokenReviewUser `json:"user,omitempty"`
		Audiences     []string        `json:"audiences,omitempty"`
	} `json:"status"`
}

type tokenReviewUser struct {
	Username string   `json:"username"`
	UID      string   `json:"uid"`
	Groups   []string `json:"groups,omitempty"`
}

// ValidateSAJWTMiddleware returns an HTTP middleware that validates Kubernetes
// service account tokens against the TokenReview API.
func ValidateSAJWTMiddleware(requiredSAUsernames []string, tokenReviewURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := extractBearerToken(r)
			if err != nil {
				slog.Warn("temenos: auth token missing or invalid", "err", err)
				writeAuthError(w, http.StatusUnauthorized, "authorization required")
				return
			}

			username, err := validateWithTokenReview(r.Context(), token, tokenReviewURL)
			if err != nil {
				slog.Warn("temenos: auth token review failed", "err", err)
				writeAuthError(w, http.StatusForbidden, "access denied — token validation failed")
				return
			}

			if !isRequiredSA(username, requiredSAUsernames) {
				slog.Warn("temenos: auth caller not in required service accounts",
					"caller", username,
					"required", requiredSAUsernames)
				writeAuthError(w, http.StatusForbidden, "access denied — unauthorized service account")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractBearerToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", ErrTokenMissing
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", ErrInvalidAuthHeader
	}
	token := strings.TrimPrefix(auth, prefix)
	if token == "" {
		return "", ErrTokenMissing
	}
	return token, nil
}

func validateWithTokenReview(ctx context.Context, token, tokenReviewURL string) (string, error) {
	reqBody := tokenReviewRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
	}
	reqBody.Spec.Token = token

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal token review: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenReviewURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("build token review request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("token review request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("token review returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var trResp tokenReviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&trResp); err != nil {
		return "", fmt.Errorf("decode token review response: %w", err)
	}

	if !trResp.Status.Authenticated {
		return "", fmt.Errorf("token not authenticated: %s", trResp.Status.Error)
	}

	if trResp.Status.User.Username == "" {
		return "", errors.New("token review response missing username")
	}

	return trResp.Status.User.Username, nil
}

func isRequiredSA(username string, requiredSAs []string) bool {
	if len(requiredSAs) == 0 {
		return true
	}
	for _, sa := range requiredSAs {
		if username == sa {
			return true
		}
	}
	return false
}

func writeAuthError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
