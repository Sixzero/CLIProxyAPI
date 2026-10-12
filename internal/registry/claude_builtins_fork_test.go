package registry

import "testing"

// Fork: built-ins fill catalog gaps and never duplicate or override a catalog entry.
func TestWithClaudeBuiltinsFillsGapsOnly(t *testing.T) {
	got := withClaudeBuiltins(nil)
	if len(got) != 1 || got[0].ID != "claude-haiku-5-5" {
		t.Fatalf("empty catalog: want claude-haiku-5-5 built-in, got %+v", got)
	}

	catalog := []*ModelInfo{{ID: "claude-haiku-5-5", DisplayName: "from catalog"}}
	got = withClaudeBuiltins(catalog)
	if len(got) != 1 || got[0].DisplayName != "from catalog" {
		t.Fatalf("catalog entry must win, got %+v", got)
	}

	if LookupStaticModelInfo("claude-haiku-5-5") == nil {
		t.Fatal("LookupStaticModelInfo must see the built-in")
	}
}
