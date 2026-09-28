package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
)

func TestApplyNoneUsesBetweenToolsForSonnet55(t *testing.T) {
	levels := &registry.ThinkingSupport{ZeroAllowed: true, Levels: []string{"low", "high"}}
	cases := map[string]string{
		"claude-sonnet-5-5": "between_tools",
		"claude-sonnet-5":   "disabled",
	}
	for id, want := range cases {
		out, err := (&Applier{}).Apply([]byte(`{"thinking":{"type":"adaptive","display":"summarized"}}`),
			thinking.ThinkingConfig{Mode: thinking.ModeNone}, &registry.ModelInfo{ID: id, Thinking: levels})
		if err != nil {
			t.Fatal(err)
		}
		if got := gjson.GetBytes(out, "thinking.type").String(); got != want {
			t.Errorf("%s: thinking.type = %q, want %q", id, got, want)
		}
		if gjson.GetBytes(out, "thinking.display").Exists() {
			t.Errorf("%s: thinking.display must be removed", id)
		}
	}
}
