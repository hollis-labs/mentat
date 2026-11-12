// Package settings reads the app's own "mentat" block from the config. The shell modules read
// theirs; this block holds what only Mentat has: the embedding profiles and the identity the CLI
// records on audit writes. The defaults live in cmd/mentat/substrate.yaml; operators override any
// key in the YAML file named by SUBSTRATE_CONFIG. This package only decodes and validates.
//
//	mentat:
//	  allowed_hosts: []         # extra Host values (besides loopback) served behind a local TLS proxy
//	  embed:
//	    small:  { provider: openai, model: text-embedding-3-small }
//	    medium: { provider: openai, model: text-embedding-3-large }
//	    full:   { provider: openai, model: text-embedding-3-large }
//	    openai_base_url: ""     # alternative OpenAI-compatible endpoint
//	    openai_key_ref: ""      # secret reference of the embedding API key (for example file://... or env://NAME with secrets.env)
//	  github_token_ref: ""      # secret reference of a GitHub token for repo stats (optional; unauthenticated without it)
//	  schedule_timezone: UTC    # IANA zone the cron fields of schedules are read in
//	  actor:
//	    id: ""                  # audit provenance actor for CLI writes (default: the OS user)
//	    model: ""               # audit provenance model name for CLI and stdio MCP writes
package settings

import (
	"fmt"
	"sort"
	"strings"
)

// Key is the name of the config block.
const Key = "mentat"

// Profile maps an embedding profile name to a provider and model.
type Profile struct {
	Provider string
	Model    string
}

// Settings are the parsed block.
type Settings struct {
	Profiles       map[string]Profile
	OpenAIBaseURL  string
	OpenAIKeyRef   string
	GitHubTokenRef string
	AllowedHosts   []string
	ScheduleZone   string
	ActorID        string
	ActorModel     string
}

// Parse reads the block (nil for a missing one). An unknown key or a wrong type is an error that
// names the key, so a typo is found at startup.
func Parse(raw any) (Settings, error) {
	s := Settings{Profiles: map[string]Profile{}}
	if raw == nil {
		return s, nil
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return s, fmt.Errorf("mentat: the config block must be a map")
	}
	if err := onlyKeys("mentat", block, "embed", "github_token_ref", "actor", "allowed_hosts", "schedule_timezone"); err != nil {
		return s, err
	}
	var err error
	if s.GitHubTokenRef, err = str(block, "mentat", "github_token_ref"); err != nil {
		return s, err
	}
	if v, present := block["allowed_hosts"]; present && v != nil {
		list, ok := v.([]any)
		if !ok {
			return s, fmt.Errorf("mentat.allowed_hosts must be a list of strings")
		}
		for _, item := range list {
			h, ok := item.(string)
			if !ok || strings.TrimSpace(h) == "" {
				return s, fmt.Errorf("mentat.allowed_hosts must be a list of non-empty strings")
			}
			s.AllowedHosts = append(s.AllowedHosts, strings.TrimSpace(h))
		}
	}
	if v, present := block["embed"]; present && v != nil {
		embed, ok := v.(map[string]any)
		if !ok {
			return s, fmt.Errorf("mentat.embed must be a map")
		}
		if err = onlyKeys("mentat.embed", embed, "small", "medium", "full", "openai_base_url", "openai_key_ref"); err != nil {
			return s, err
		}
		if s.OpenAIBaseURL, err = str(embed, "mentat.embed", "openai_base_url"); err != nil {
			return s, err
		}
		if s.OpenAIKeyRef, err = str(embed, "mentat.embed", "openai_key_ref"); err != nil {
			return s, err
		}
		for _, name := range []string{"small", "medium", "full"} {
			pv, present := embed[name]
			if !present || pv == nil {
				continue
			}
			pm, ok := pv.(map[string]any)
			if !ok {
				return s, fmt.Errorf("mentat.embed.%s must be a map", name)
			}
			where := "mentat.embed." + name
			if err = onlyKeys(where, pm, "provider", "model"); err != nil {
				return s, err
			}
			var p Profile
			if p.Provider, err = str(pm, where, "provider"); err != nil {
				return s, err
			}
			if p.Model, err = str(pm, where, "model"); err != nil {
				return s, err
			}
			if p.Provider == "" || p.Model == "" {
				return s, fmt.Errorf("%s needs a provider and a model", where)
			}
			s.Profiles[name] = p
		}
	}
	if s.ScheduleZone, err = str(block, "mentat", "schedule_timezone"); err != nil {
		return s, err
	}
	if v, present := block["actor"]; present && v != nil {
		actor, ok := v.(map[string]any)
		if !ok {
			return s, fmt.Errorf("mentat.actor must be a map")
		}
		if err = onlyKeys("mentat.actor", actor, "id", "model"); err != nil {
			return s, err
		}
		if s.ActorID, err = str(actor, "mentat.actor", "id"); err != nil {
			return s, err
		}
		if s.ActorModel, err = str(actor, "mentat.actor", "model"); err != nil {
			return s, err
		}
	}
	return s, nil
}

func str(m map[string]any, where, key string) (string, error) {
	v, present := m[key]
	if !present || v == nil {
		return "", nil
	}
	t, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s.%s must be a string", where, key)
	}
	return strings.TrimSpace(t), nil
}

func onlyKeys(where string, m map[string]any, known ...string) error {
	ok := map[string]bool{}
	for _, k := range known {
		ok[k] = true
	}
	var unknown []string
	for k := range m {
		if !ok[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%s: unknown config key(s): %s", where, strings.Join(unknown, ", "))
}
