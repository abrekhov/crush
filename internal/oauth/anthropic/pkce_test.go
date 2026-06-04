package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewAuthRequest(t *testing.T) {
	req, err := NewAuthRequest()
	if err != nil {
		t.Fatalf("NewAuthRequest: %v", err)
	}

	if req.Verifier == "" {
		t.Error("Verifier must not be empty")
	}
	if req.State == "" {
		t.Error("State must not be empty")
	}
	if !strings.HasPrefix(req.URL, "https://claude.ai/oauth/authorize?") {
		t.Errorf("unexpected auth URL prefix: %s", req.URL)
	}
	for _, param := range []string{"code_challenge=", "code_challenge_method=S256", "client_id=", "redirect_uri=", "scope="} {
		if !strings.Contains(req.URL, param) {
			t.Errorf("auth URL missing param %q", param)
		}
	}
}

func TestNewAuthRequest_Unique(t *testing.T) {
	a, _ := NewAuthRequest()
	b, _ := NewAuthRequest()
	if a.Verifier == b.Verifier {
		t.Error("verifiers must be unique across calls")
	}
	if a.State == b.State {
		t.Error("states must be unique across calls")
	}
}

func TestHeaders(t *testing.T) {
	h := Headers()
	v, ok := h["anthropic-beta"]
	if !ok {
		t.Fatal("Headers() must include anthropic-beta key")
	}
	if !strings.Contains(v, "oauth-2025-04-20") {
		t.Errorf("anthropic-beta header value %q missing oauth-2025-04-20", v)
	}
}

func withTestTokenServer(t *testing.T, handler http.HandlerFunc) func() {
	t.Helper()
	srv := httptest.NewServer(handler)
	orig := tokenURL
	tokenURL = srv.URL
	return func() {
		tokenURL = orig
		srv.Close()
	}
}

func TestExchangeCode_Success(t *testing.T) {
	cleanup := withTestTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		if r.Form.Get("code") == "" {
			t.Error("code must not be empty")
		}
		if r.Form.Get("code_verifier") == "" {
			t.Error("code_verifier must not be empty")
		}
		if r.Form.Get("client_id") != ClientID {
			t.Errorf("client_id = %q, want %s", r.Form.Get("client_id"), ClientID)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "acc_test",
			"refresh_token": "ref_test",
			"expires_in":    3600,
		})
	})
	defer cleanup()

	token, err := ExchangeCode(context.Background(), "testcode", "testverifier")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token.AccessToken != "acc_test" {
		t.Errorf("AccessToken = %q, want acc_test", token.AccessToken)
	}
	if token.RefreshToken != "ref_test" {
		t.Errorf("RefreshToken = %q, want ref_test", token.RefreshToken)
	}
	if token.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", token.ExpiresIn)
	}
	if token.ExpiresAt == 0 {
		t.Error("ExpiresAt must be set")
	}
}

func TestExchangeCode_ErrorResponse(t *testing.T) {
	cleanup := withTestTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "authorization code expired",
		})
	})
	defer cleanup()

	_, err := ExchangeCode(context.Background(), "badcode", "verifier")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authorization code expired") {
		t.Errorf("error should mention the description, got: %v", err)
	}
}

func TestRefreshToken_Success(t *testing.T) {
	cleanup := withTestTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if r.Form.Get("refresh_token") != "old_refresh" {
			t.Errorf("refresh_token = %q", r.Form.Get("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new_acc",
			"refresh_token": "new_ref",
			"expires_in":    7200,
		})
	})
	defer cleanup()

	token, err := RefreshToken(context.Background(), "old_refresh")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if token.AccessToken != "new_acc" {
		t.Errorf("AccessToken = %q, want new_acc", token.AccessToken)
	}
}

func TestRefreshToken_ErrorResponse(t *testing.T) {
	cleanup := withTestTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "refresh token has expired",
		})
	})
	defer cleanup()

	_, err := RefreshToken(context.Background(), "stale_token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "refresh token has expired") {
		t.Errorf("error should mention the description, got: %v", err)
	}
}
