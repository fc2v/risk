package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRealConfig(t *testing.T) {
	cfg, err := Load("../config.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) == 0 || cfg.History.Lookback <= 0 {
		t.Errorf("loaded %d rules, lookback %v", len(cfg.Rules), cfg.History.Lookback)
	}
	for _, ri := range cfg.Rules {
		if ri.Params == nil {
			t.Errorf("rule %s has no params", ri.Name)
		}
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct {
		edit    func(cfg map[string]any)
		wantErr string
	}{
		"unknown top-level key":   {func(c map[string]any) { c["extra"] = 1 }, "unknown field"},
		"bad duration":            {func(c map[string]any) { c["history"] = map[string]any{"lookback": "90 days"} }, `invalid duration "90 days": use a Go duration`},
		"duration not a string":   {func(c map[string]any) { c["history"] = map[string]any{"lookback": 3600} }, "invalid duration 3600: use a Go duration"},
		"no lookback":             {func(c map[string]any) { c["history"] = map[string]any{} }, "history.lookback"},
		"bands out of order":      {func(c map[string]any) { c["bands"] = map[string]any{"minMedium": 60, "minHigh": 30} }, "minHigh must be > bands.minMedium"},
		"no topic":                {func(c map[string]any) { consumer(c)["topic"] = "" }, "consumer.topic"},
		"dlq same as topic":       {func(c map[string]any) { consumer(c)["deadLetterTopic"] = consumer(c)["topic"] }, "must differ"},
		"retry never fails":       {func(c map[string]any) { retry(c)["failAfterAttempts"] = 0 }, "failAfterAttempts"},
		"max below initial":       {func(c map[string]any) { retry(c)["maxBackoff"] = "1ms" }, "maxBackoff"},
		"unknown rule":            {func(c map[string]any) { rule(c, 0)["rule"] = "magic" }, "magic"},
		"param of another rule":   {func(c map[string]any) { rule(c, 0)["maxPayeeAge"] = "1h" }, "maxPayeeAge"},
		"missing name":            {func(c map[string]any) { rule(c, 0)["name"] = "" }, "name must be set"},
		"duplicate name":          {func(c map[string]any) { rule(c, 1)["name"] = rule(c, 0)["name"] }, "must be unique"},
		"score above 100":         {func(c map[string]any) { rule(c, 0)["score"] = 101 }, "score must be between"},
		"unknown kind":            {func(c map[string]any) { rule(c, 0)["appliesTo"] = []string{"cheque"} }, "unknown kind"},
		"empty appliesTo":         {func(c map[string]any) { rule(c, 0)["appliesTo"] = []string{} }, "appliesTo must list"},
		"unknown learnsFrom kind": {func(c map[string]any) { rule(c, 0)["learnsFrom"] = []string{"cheque"} }, "learnsFrom"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw := realConfig(t)
			tc.edit(raw)
			_, err := Load(writeConfig(t, raw))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Load() error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestDisabledRuleSkipsValidation(t *testing.T) {
	raw := realConfig(t)
	rule(raw, 0)["enabled"] = false
	rule(raw, 0)["score"] = 500
	if _, err := Load(writeConfig(t, raw)); err != nil {
		t.Errorf("disabled rule with an out-of-range score rejected: %v", err)
	}
}

func realConfig(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../config.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeConfig(t *testing.T, raw map[string]any) string {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func consumer(c map[string]any) map[string]any { return c["consumer"].(map[string]any) }
func retry(c map[string]any) map[string]any    { return consumer(c)["retry"].(map[string]any) }
func rule(c map[string]any, i int) map[string]any {
	return c["rules"].([]any)[i].(map[string]any)
}
