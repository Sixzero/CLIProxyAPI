package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDisableImageGenerationMode_UnmarshalYAML(t *testing.T) {
	type wrapper struct {
		V DisableImageGenerationMode `yaml:"disable-image-generation"`
	}

	{
		var w wrapper
		if err := yaml.Unmarshal([]byte("disable-image-generation: false\n"), &w); err != nil {
			t.Fatalf("unmarshal false: %v", err)
		}
		if w.V != DisableImageGenerationOff {
			t.Fatalf("false => %v, want %v", w.V, DisableImageGenerationOff)
		}
	}

	{
		var w wrapper
		if err := yaml.Unmarshal([]byte("disable-image-generation: true\n"), &w); err != nil {
			t.Fatalf("unmarshal true: %v", err)
		}
		if w.V != DisableImageGenerationAll {
			t.Fatalf("true => %v, want %v", w.V, DisableImageGenerationAll)
		}
	}

	{
		var w wrapper
		if err := yaml.Unmarshal([]byte("disable-image-generation: chat\n"), &w); err != nil {
			t.Fatalf("unmarshal chat: %v", err)
		}
		if w.V != DisableImageGenerationChat {
			t.Fatalf("chat => %v, want %v", w.V, DisableImageGenerationChat)
		}
	}

	{
		var w wrapper
		if err := yaml.Unmarshal([]byte("disable-image-generation: passthrough\n"), &w); err != nil {
			t.Fatalf("unmarshal passthrough: %v", err)
		}
		if w.V != DisableImageGenerationPassthrough {
			t.Fatalf("passthrough => %v, want %v", w.V, DisableImageGenerationPassthrough)
		}
	}
}

func TestDisableImageGenerationMode_UnmarshalJSON(t *testing.T) {
	{
		var v DisableImageGenerationMode
		if err := json.Unmarshal([]byte("false"), &v); err != nil {
			t.Fatalf("unmarshal false: %v", err)
		}
		if v != DisableImageGenerationOff {
			t.Fatalf("false => %v, want %v", v, DisableImageGenerationOff)
		}
	}

	{
		var v DisableImageGenerationMode
		if err := json.Unmarshal([]byte("true"), &v); err != nil {
			t.Fatalf("unmarshal true: %v", err)
		}
		if v != DisableImageGenerationAll {
			t.Fatalf("true => %v, want %v", v, DisableImageGenerationAll)
		}
	}

	{
		var v DisableImageGenerationMode
		if err := json.Unmarshal([]byte(`"chat"`), &v); err != nil {
			t.Fatalf("unmarshal chat: %v", err)
		}
		if v != DisableImageGenerationChat {
			t.Fatalf("chat => %v, want %v", v, DisableImageGenerationChat)
		}
	}

	{
		var v DisableImageGenerationMode
		if err := json.Unmarshal([]byte(`"passthrough"`), &v); err != nil {
			t.Fatalf("unmarshal passthrough: %v", err)
		}
		if v != DisableImageGenerationPassthrough {
			t.Fatalf("passthrough => %v, want %v", v, DisableImageGenerationPassthrough)
		}
	}
}

// Both YAML layouts and both loading paths must opt in to image tool injection.
func TestDisableImageGenerationConfigDefault(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    DisableImageGenerationMode
	}{
		{"omitted", "{}\n", DisableImageGenerationPassthrough},
		{"multimedia without mode", "multimedia:\n  gpt-image-2-base-model: gpt-5.4-mini\n", DisableImageGenerationPassthrough},
		{"null mode", "multimedia:\n  disable-image-generation: null\n", DisableImageGenerationPassthrough},
		{"legacy opt in", "disable-image-generation: false\n", DisableImageGenerationOff},
		{"v8 opt in", "multimedia:\n  disable-image-generation: false\n", DisableImageGenerationOff},
		{"legacy disable all", "disable-image-generation: true\n", DisableImageGenerationAll},
		{"v8 disable all", "multimedia:\n  disable-image-generation: true\n", DisableImageGenerationAll},
		{"chat", "multimedia:\n  disable-image-generation: chat\n", DisableImageGenerationChat},
		{"passthrough", "multimedia:\n  disable-image-generation: passthrough\n", DisableImageGenerationPassthrough},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseConfigBytes([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.payload), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			for name, cfg := range map[string]*Config{"parse": parsed, "load": loaded} {
				if cfg.DisableImageGeneration != tc.want {
					t.Errorf("%s: got %v, want %v", name, cfg.DisableImageGeneration, tc.want)
				}
			}
		})
	}
}

func TestDisableImageGenerationOptionalConfigDefault(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		missing       bool
	}{
		{name: "missing", missing: true},
		{name: "empty"},
		{name: "invalid", payload: "["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.payload), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := LoadConfigOptional(path, true)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DisableImageGeneration != DisableImageGenerationPassthrough {
				t.Fatalf("got %v, want passthrough", cfg.DisableImageGeneration)
			}
		})
	}
}
