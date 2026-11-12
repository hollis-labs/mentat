package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	embedcontracts "github.com/hollis-labs/substrate/llm-core/embedcontracts"
	"github.com/openai/openai-go/option"
)

type ProfileConfig struct {
	Name     string
	Provider string
	Model    string
}

// Options configure a Manager. The caller resolves settings and secrets; this package reads no
// environment.
type Options struct {
	// Profiles maps a profile name (small, medium, full) to its provider and model.
	Profiles map[string]ProfileConfig
	// OpenAIKey and OpenAIBaseURL configure the openai provider. An empty key fails at call time.
	OpenAIKey     string
	OpenAIBaseURL string
}

type Manager struct {
	profiles map[string]ProfileConfig
	byModel  map[string]ProfileConfig
	opts     Options
}

func NewManager(opts Options) *Manager {
	profiles := make(map[string]ProfileConfig, len(opts.Profiles))
	for name, cfg := range opts.Profiles {
		cfg.Name = name
		profiles[name] = cfg
	}
	byModel := make(map[string]ProfileConfig, len(profiles))
	for _, cfg := range profiles {
		byModel[cfg.Model] = cfg
	}
	return &Manager{profiles: profiles, byModel: byModel, opts: opts}
}

func (m *Manager) Profile(name string) (ProfileConfig, error) {
	if name == "" || name == "none" {
		return ProfileConfig{}, fmt.Errorf("embedding profile %q is disabled", name)
	}
	cfg, ok := m.profiles[name]
	if !ok {
		return ProfileConfig{}, fmt.Errorf("unknown embedding profile %q", name)
	}
	return cfg, nil
}

func (m *Manager) ResolveModel(model string) (ProfileConfig, error) {
	cfg, ok := m.byModel[model]
	if !ok {
		return ProfileConfig{}, fmt.Errorf("no provider mapping configured for model %q", model)
	}
	return cfg, nil
}

func (m *Manager) Embed(ctx context.Context, providerName, model, text string) ([]float32, error) {
	embedder, err := m.newProvider(providerName)
	if err != nil {
		return nil, err
	}
	res, err := embedder.Embed(ctx, text, model)
	if err != nil {
		return nil, err
	}
	return res.Embedding, nil
}

func (m *Manager) EmbedBatch(ctx context.Context, providerName, model string, texts []string) ([][]float32, error) {
	embedder, err := m.newProvider(providerName)
	if err != nil {
		return nil, err
	}
	results, err := embedder.EmbedBatch(ctx, texts, model)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(results))
	for i := range results {
		out[i] = results[i].Embedding
	}
	return out, nil
}

func HashText(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			_, _ = h.Write([]byte(trimmed))
			_, _ = h.Write([]byte{'\n'})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Manager) newProvider(name string) (embedcontracts.Embedder, error) {
	switch strings.ToLower(name) {
	case "openai":
		var opts []option.RequestOption
		if m.opts.OpenAIBaseURL != "" {
			opts = append(opts, option.WithBaseURL(m.opts.OpenAIBaseURL))
		}
		return NewOpenAIEmbedder(m.opts.OpenAIKey, nil, opts...), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q (only %q is supported)", name, "openai")
	}
}
