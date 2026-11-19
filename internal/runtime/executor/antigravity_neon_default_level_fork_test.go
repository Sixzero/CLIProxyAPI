package executor

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// FORK: our config.yaml defaults gemini-4-neon to thinkingLevel=high via a
// payload.default rule (bare xhigh can spend the whole 65k budget thinking and
// return nothing). Pin that the rule lands upstream and a (level) suffix wins.
func TestAntigravityNeonPayloadDefaultThinkingLevel(t *testing.T) {
	cfg := &config.Config{}
	cfg.Payload.Default = []config.PayloadRule{{
		Models: []config.PayloadModelRule{{Name: "gemini-4-neon*", Protocol: "antigravity",
			NotExist: []string{"generationConfig.thinkingConfig.thinkingLevel", "generationConfig.thinkingConfig.thinkingBudget"}}},
		Params: map[string]any{"generationConfig.thinkingConfig.thinkingLevel": "high"},
	}}
	if registry.LookupModelInfo("gemini-4-neon-xhigh", "antigravity") == nil {
		t.Skip("gemini-4-neon-xhigh not in the embedded catalog")
	}
	for _, tt := range []struct{ model, want string }{
		{"gemini-4-neon-xhigh", "high"},
		{"gemini-4-neon-xhigh(low)", "low"},
		{"gemini-4-neon-xhigh(xhigh)", "xhigh"},
	} {
		t.Run(tt.model, func(t *testing.T) {
			c := newIssue6825Conversation(t)
			c.executor = NewAntigravityExecutor(cfg)
			payload := []byte(`{"model":"` + tt.model + `","messages":[{"role":"user","content":"hi"}]}`)
			req := cliproxyexecutor.Request{Model: tt.model, Payload: payload}
			opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, OriginalRequest: payload}
			if _, err := c.executor.Execute(t.Context(), c.auth, req, opts); err != nil && !strings.Contains(err.Error(), "Done") {
				t.Fatalf("Execute: %v", err)
			}
			up := <-c.requests
			if got := ojson.GetBytes(up.body, "request.generationConfig.thinkingConfig.thinkingLevel").String(); got != tt.want {
				t.Fatalf("thinkingLevel = %q, want %q; body=%s", got, tt.want, up.body)
			}
		})
	}
}
