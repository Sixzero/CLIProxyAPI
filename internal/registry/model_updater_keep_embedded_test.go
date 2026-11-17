package registry

import (
	"encoding/json"
	"testing"
)

// A remote catalog lacking a model the fork embedded must not drop it.
func TestRemoteCatalogKeepsEmbeddedOnlyModels(t *testing.T) {
	var embedded staticModelsJSON
	if err := json.Unmarshal(embeddedModelsJSON, &embedded); err != nil {
		t.Fatal(err)
	}
	remote := embedded
	var dropped string
	for i, m := range remote.CodexPro {
		if m != nil && m.ID == "gpt-6.2-sol" {
			dropped = m.ID
			remote.CodexPro = append(append([]*ModelInfo{}, remote.CodexPro[:i]...), remote.CodexPro[i+1:]...)
			break
		}
	}
	if dropped == "" {
		t.Fatal("embedded codex-pro lacks gpt-6.2-sol")
	}
	keepEmbeddedOnlyModels(&remote)
	n := 0
	for _, m := range remote.CodexPro {
		if m != nil && m.ID == dropped {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want %s once, got %d", dropped, n)
	}
}
