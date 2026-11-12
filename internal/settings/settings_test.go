package settings

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	s, err := Parse(nil)
	if err != nil || len(s.Profiles) != 0 {
		t.Fatalf("empty: %+v %v", s, err)
	}
	s, err = Parse(map[string]any{
		"embed": map[string]any{
			"small":           map[string]any{"provider": "openai", "model": "m1"},
			"full":            map[string]any{"provider": "openai", "model": "text-embedding-3-large"},
			"openai_base_url": "http://127.0.0.1:1/v1",
			"openai_key_ref":  "file://key",
		},
		"github_token_ref": "file://gh",
		"allowed_hosts":    []any{"mentat.example.test"},
		"actor":            map[string]any{"id": "a", "model": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Profiles["small"].Model != "m1" || s.Profiles["small"].Provider != "openai" || s.Profiles["full"].Model != "text-embedding-3-large" || len(s.Profiles) != 2 {
		t.Fatalf("profiles: %+v", s.Profiles)
	}
	if s.OpenAIBaseURL == "" || s.OpenAIKeyRef != "file://key" || s.GitHubTokenRef != "file://gh" || s.ActorID != "a" || s.ActorModel != "x" || len(s.AllowedHosts) != 1 {
		t.Fatalf("settings: %+v", s)
	}
}

func TestParseRefusesBadValues(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want string
	}{
		"unknown top key":   {map[string]any{"emded": map[string]any{}}, "emded"},
		"unknown embed key": {map[string]any{"embed": map[string]any{"smal": map[string]any{}}}, "mentat.embed: unknown config key(s): smal"},
		"wrong type":        {map[string]any{"embed": map[string]any{"openai_base_url": 5}}, "mentat.embed.openai_base_url must be a string"},
		"bad hosts":         {map[string]any{"allowed_hosts": "x"}, "mentat.allowed_hosts must be a list"},
		"half a profile":    {map[string]any{"embed": map[string]any{"small": map[string]any{"model": "m"}}}, "mentat.embed.small needs a provider and a model"},
		"not a map":         {"x", "must be a map"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
