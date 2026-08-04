package config

import (
	"strings"
	"testing"

	anthropicauth "github.com/abrekhov/crush/internal/oauth/anthropic"
)

func TestSetupAnthropicOAuth(t *testing.T) {
	t.Run("rewrites the raw token into the Bearer form", func(t *testing.T) {
		c := &ProviderConfig{APIKey: "sk-ant-oat-abc123"}
		c.SetupAnthropicOAuth()

		// buildAnthropicProvider keys off this exact prefix to send an
		// Authorization header instead of x-api-key.
		if got, want := c.APIKey, "Bearer sk-ant-oat-abc123"; got != want {
			t.Errorf("APIKey = %q, want %q", got, want)
		}
	})

	t.Run("sets the oauth beta header", func(t *testing.T) {
		c := &ProviderConfig{APIKey: "tok"}
		c.SetupAnthropicOAuth()

		if got, want := c.ExtraHeaders["anthropic-beta"], anthropicauth.OAuthBetaHeader; got != want {
			t.Errorf("anthropic-beta = %q, want %q", got, want)
		}
	})

	t.Run("leads the system prompt with the Claude Code identity", func(t *testing.T) {
		c := &ProviderConfig{APIKey: "tok"}
		c.SetupAnthropicOAuth()

		if !strings.HasPrefix(c.SystemPromptPrefix, anthropicauth.ClaudeCodeIdentity) {
			t.Errorf("SystemPromptPrefix = %q, want it to start with the Claude Code identity", c.SystemPromptPrefix)
		}
	})

	t.Run("preserves a user-configured system prompt prefix after the identity", func(t *testing.T) {
		c := &ProviderConfig{APIKey: "tok", SystemPromptPrefix: "Always answer in Portuguese."}
		c.SetupAnthropicOAuth()

		if !strings.HasPrefix(c.SystemPromptPrefix, anthropicauth.ClaudeCodeIdentity) {
			t.Errorf("identity must lead, got %q", c.SystemPromptPrefix)
		}
		if !strings.Contains(c.SystemPromptPrefix, "Always answer in Portuguese.") {
			t.Errorf("user prefix was dropped: %q", c.SystemPromptPrefix)
		}
	})

	// A config reload re-runs setup over an already-prepared provider, so
	// applying it twice must not double the prefix or the identity block.
	t.Run("is idempotent", func(t *testing.T) {
		c := &ProviderConfig{APIKey: "tok", SystemPromptPrefix: "Be brief."}
		c.SetupAnthropicOAuth()
		first := *c
		c.SetupAnthropicOAuth()

		if c.APIKey != first.APIKey {
			t.Errorf("APIKey changed on second call: %q -> %q", first.APIKey, c.APIKey)
		}
		if c.SystemPromptPrefix != first.SystemPromptPrefix {
			t.Errorf("SystemPromptPrefix changed on second call: %q -> %q", first.SystemPromptPrefix, c.SystemPromptPrefix)
		}
		if n := strings.Count(c.APIKey, "Bearer "); n != 1 {
			t.Errorf("expected exactly one Bearer prefix, got %d in %q", n, c.APIKey)
		}
	})

	t.Run("does not add a Bearer prefix to an empty key", func(t *testing.T) {
		c := &ProviderConfig{}
		c.SetupAnthropicOAuth()

		if c.APIKey != "" {
			t.Errorf("APIKey = %q, want empty", c.APIKey)
		}
	})
}
