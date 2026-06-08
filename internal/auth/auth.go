package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const tokenReviewPath = "/apis/authentication.k8s.io/v1/tokenreviews"

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

func ValidateToken(ctx context.Context, token, baseURL string) (string, error) {
	reqBody := tokenReviewRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
	}
	reqBody.Spec.Token = token

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal token review: %w", err)
	}

	url := baseURL + tokenReviewPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("build token review request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("token review request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

func IsRequiredSA(username string, requiredSAs []string) bool {
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
