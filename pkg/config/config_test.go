package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithProfileAppliesOverrides(t *testing.T) {
	cfg := Default()
	cfg.Model = "base-model"
	enabled := true
	cfg.Profiles["custom"] = Profile{
		Provider:       "openai",
		Model:          "custom-model",
		Reasoning:      "low",
		MaxSteps:       12,
		ApprovalMode:   "ask",
		StoreResponses: &enabled,
	}

	got, err := cfg.WithProfile("custom")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" || got.Model != "custom-model" || got.MaxSteps != 12 || got.ApprovalMode != "ask" || !got.StoreResponses {
		t.Fatalf("profile not applied: %+v", got)
	}
}

func TestWithProfileClearsInheritedModelWhenProviderChanges(t *testing.T) {
	cfg := Default()
	got, err := cfg.WithProfile("coding")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", got.Provider)
	}
	if got.Model != "" {
		t.Fatalf("expected model to be resolved from provider env, got %q", got.Model)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "mock" || cfg.MaxSteps == 0 || cfg.MaxContext == 0 || !cfg.Stream || !cfg.ContextCockpit || !cfg.ContextCockpitInject || !cfg.ContextRetrieval || !cfg.ContextRecipes || !cfg.NegativeContext || !cfg.Skills {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
	if cfg.ContextCockpitMaxCards != 10 || cfg.ContextRetrievalTokens != 1000 || cfg.ContextRetrievalChunks != 10 || !cfg.ContextEmbeddings || cfg.ContextReranker {
		t.Fatalf("expected retrieval MVP defaults, got %+v", cfg)
	}
	if len(cfg.DisabledTools) != 1 || cfg.DisabledTools[0] != "write_file" {
		t.Fatalf("expected write_file disabled by default, got %+v", cfg.DisabledTools)
	}
}

func TestLoadOldConfigDefaultsNewContextBooleansOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"provider":"mock","model":"mock-agent"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Stream || !cfg.ContextCockpit || !cfg.ContextCockpitInject || !cfg.ContextCockpitDiff || !cfg.ContextRetrieval || !cfg.ContextEmbeddings || cfg.ContextReranker || !cfg.ContextRecipes || !cfg.NegativeContext || !cfg.Skills {
		t.Fatalf("expected missing new context booleans to default on: %+v", cfg)
	}
}

func TestWithProfileAppliesContextCockpitOverrides(t *testing.T) {
	cfg := Default()
	disabled := false
	cfg.Profiles["small"] = Profile{
		Stream:                 &disabled,
		ContextCockpit:         &disabled,
		ContextCockpitInject:   &disabled,
		ContextCockpitTokens:   700,
		ContextCockpitMaxFiles: 25,
		ContextCockpitMaxCards: 8,
		ContextCockpitDiff:     &disabled,
		ContextRetrieval:       &disabled,
		ContextRetrievalTokens: 900,
		ContextRetrievalChunks: 6,
		ContextEmbeddings:      &disabled,
		ContextReranker:        &disabled,
		ContextRecipes:         &disabled,
		NegativeContext:        &disabled,
		Skills:                 &disabled,
	}
	got, err := cfg.WithProfile("small")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stream || got.ContextCockpit || got.ContextCockpitInject || got.ContextCockpitDiff || got.ContextRetrieval || got.ContextEmbeddings || got.ContextReranker || got.ContextRecipes || got.NegativeContext || got.Skills {
		t.Fatalf("expected cockpit booleans disabled: %+v", got)
	}
	if got.ContextCockpitTokens != 700 || got.ContextCockpitMaxFiles != 25 || got.ContextCockpitMaxCards != 8 || got.ContextRetrievalTokens != 900 || got.ContextRetrievalChunks != 6 {
		t.Fatalf("expected cockpit budgets applied: %+v", got)
	}
}

func TestWithProfileMergesMCPServers(t *testing.T) {
	cfg := Default()
	disabled := false
	cfg.MCPServers["base"] = MCPServer{Command: "base-cmd"}
	cfg.Profiles["mcp"] = Profile{
		MCPServers: map[string]MCPServer{
			"demo": {Command: "demo-cmd", Args: []string{"--stdio"}, Enabled: &disabled},
		},
	}
	got, err := cfg.WithProfile("mcp")
	if err != nil {
		t.Fatal(err)
	}
	if got.MCPServers["base"].Command != "base-cmd" || got.MCPServers["demo"].Command != "demo-cmd" || got.MCPServers["demo"].Enabled == nil {
		t.Fatalf("expected merged mcp servers: %+v", got.MCPServers)
	}
}

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	written, err := WriteDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if written != path {
		t.Fatalf("unexpected path: %s", written)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
