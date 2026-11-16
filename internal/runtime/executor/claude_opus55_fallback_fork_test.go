package executor

import (
	"context"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// FORK: the cloak's Opus 5.5 -> Opus 4.8 server-side fallback is off by
// default (see claudeOpus55FallbackEnabled). Upstream tests describing it opt in.
func enableClaudeOpus55FallbackForTest(t *testing.T) {
	t.Helper()
	t.Setenv("CPA_CLAUDE_OPUS55_FALLBACK", "1")
}

func TestOpus55FallbackDisabledByDefault(t *testing.T) {
	t.Setenv("CPA_CLAUDE_OPUS55_FALLBACK", "")
	cfg := &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "sk-ant-oat-fork", Cloak: &config.CloakConfig{}}}}
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-ant-oat-fork"}}
	for name, tt := range map[string]struct{ payload, want string }{
		"no injection":         {`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"x"}]}`, ""},
		"caller fallback kept": {`{"model":"claude-opus-5-5","fallbacks":[{"model":"claude-sonnet-5"}],"messages":[{"role":"user","content":"x"}]}`, "claude-sonnet-5"},
	} {
		t.Run(name, func(t *testing.T) {
			body, _, err := applyCloaking(context.Background(), cfg, auth, []byte(tt.payload), "sk-ant-oat-fork", false, true)
			if err != nil {
				t.Fatal(err)
			}
			if got := ojson.GetBytes(body, "fallbacks.0.model").String(); got != tt.want {
				t.Fatalf("fallback = %q, want %q", got, tt.want)
			}
			got := reconcileClaudeCodeFableModelAfterPayload(body, claudeCodeFableState{}, false, false, true, false)
			if f := ojson.GetBytes(got, "fallbacks.0.model").String(); f != tt.want {
				t.Fatalf("after reconcile fallback = %q, want %q", f, tt.want)
			}
		})
	}
}
