package executor

import (
	"os"
	"testing"

	"github.com/tidwall/gjson"
)

// FORK: the MCP tool aliasing is off by default here (see
// claudeMCPToolAliasEnabled). Upstream's tests describe the aliasing behavior,
// so the test binary opts back in and keeps them meaningful. Living in a
// fork-owned file keeps the upstream test files conflict-free on rebase.
func init() {
	_ = os.Setenv("CPA_CLAUDE_MCP_TOOL_ALIAS", "1")
}

func TestPrepareClaudeOAuthToolNamesForUpstream_DisabledByDefaultKeepsToolNames(t *testing.T) {
	t.Setenv("CPA_CLAUDE_MCP_TOOL_ALIAS", "")

	body := []byte(`{"model":"claude-opus-4-7","tools":[{"name":"bash"},{"name":"read"}],"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"bash","input":{}}]}]}`)
	out, reverseMap := prepareClaudeOAuthToolNamesForUpstream(body, claudeMCPAliasOptions{secret: "fork-caller"})

	if len(reverseMap) != 0 {
		t.Fatalf("expected no aliases, got %v", reverseMap)
	}
	for path, want := range map[string]string{
		"tools.0.name":              "bash",
		"tools.1.name":              "read",
		"messages.0.content.0.name": "bash",
	} {
		if got := gjson.GetBytes(out, path).String(); got != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
}
