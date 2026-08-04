// Package anthropic provides OAuth 2.0 + PKCE authentication for claude.ai subscriptions.
package anthropic

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abrekhov/crush/internal/oauth"
)

const (
	// ClientID is the public OAuth client identifier for Claude Code CLI.
	ClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	defaultAuthURL     = "https://claude.ai/oauth/authorize"
	defaultTokenURL    = "https://console.anthropic.com/v1/oauth/token"
	defaultRedirectURI = "https://console.anthropic.com/oauth/code/callback"
	scopes             = "org:create_api_key user:profile user:inference"

	// OAuthBetaHeader is the required beta header for API calls using OAuth tokens.
	OAuthBetaHeader = "oauth-2025-04-20"

	// ClaudeCodeIdentity must be the first system block of every request
	// authenticated with a claude.ai subscription token. The subscription
	// inference endpoint is scoped to Claude Code and rejects requests that
	// do not lead with this exact line.
	ClaudeCodeIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
)

// These vars are package-level so tests can override them.
var (
	authURL     = defaultAuthURL
	tokenURL    = defaultTokenURL
	redirectURI = defaultRedirectURI
)

// Headers returns extra HTTP headers needed for Anthropic OAuth-authenticated API calls.
func Headers() map[string]string {
	return map[string]string{
		"anthropic-beta": OAuthBetaHeader,
	}
}

// AuthRequest holds the PKCE state needed to complete an authorization flow.
type AuthRequest struct {
	Verifier string
	State    string
	URL      string
}

// NewAuthRequest creates a PKCE authorization request.
// The URL field should be opened in a browser; the Verifier is needed for ExchangeCode.
func NewAuthRequest() (*AuthRequest, error) {
	verifier, err := generateRandom(32)
	if err != nil {
		return nil, fmt.Errorf("generate verifier: %w", err)
	}
	state, err := generateRandom(32)
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {scopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}

	return &AuthRequest{
		Verifier: verifier,
		State:    state,
		URL:      authURL + "?" + params.Encode(),
	}, nil
}

func generateRandom(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// tokenResponse is the raw JSON body from the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// ExchangeCode exchanges an authorization code (from the redirect URL) for tokens.
//
// The callback page renders the code as "<code>#<state>", and that is what
// users copy out of the address bar, so the fragment is split off here and
// the state is forwarded along with the exchange.
func ExchangeCode(ctx context.Context, code, verifier string) (*oauth.Token, error) {
	code, state, _ := strings.Cut(code, "#")
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	if state != "" {
		data.Set("state", state)
	}
	return postToken(ctx, data)
}

// RefreshToken exchanges a refresh token for a fresh access token.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {refreshToken},
	}
	return postToken(ctx, data)
}

func postToken(ctx context.Context, data url.Values) (*oauth.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "crush")

	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w: %s", err, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		if tr.ErrorDesc != "" {
			return nil, fmt.Errorf("token request failed: %s", tr.ErrorDesc)
		}
		return nil, fmt.Errorf("token request failed: status %d body %q", resp.StatusCode, string(body))
	}

	t := &oauth.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresIn:    tr.ExpiresIn,
	}
	t.SetExpiresAt()
	return t, nil
}
