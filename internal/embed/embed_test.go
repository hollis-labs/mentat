package embed

import "testing"

func TestNewManagerServesTheGivenProfiles(t *testing.T) {

	m := NewManager(Options{Profiles: map[string]ProfileConfig{
		"small":  {Provider: "openai", Model: "text-embedding-3-small"},
		"medium": {Provider: "openai", Model: "text-embedding-3-large"},
		"full":   {Provider: "openai", Model: "text-embedding-3-large"},
	}})

	small, err := m.Profile("small")
	if err != nil {
		t.Fatalf("small profile: %v", err)
	}
	if small.Provider != "openai" || small.Model != "text-embedding-3-small" {
		t.Fatalf("small profile = %#v", small)
	}

	medium, err := m.Profile("medium")
	if err != nil {
		t.Fatalf("medium profile: %v", err)
	}
	if medium.Provider != "openai" || medium.Model != "text-embedding-3-large" {
		t.Fatalf("medium profile = %#v", medium)
	}

	full, err := m.Profile("full")
	if err != nil {
		t.Fatalf("full profile: %v", err)
	}
	if full.Provider != "openai" || full.Model != "text-embedding-3-large" {
		t.Fatalf("full profile = %#v", full)
	}
}

func TestNewProviderRejectsNonOpenAIProviders(t *testing.T) {
	if _, err := NewManager(Options{}).newProvider("ollama"); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
