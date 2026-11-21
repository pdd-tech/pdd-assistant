package config

import (
	"strings"
	"testing"
	"time"
)

func TestServerSettings(t *testing.T) {
	for _, name := range []string{
		"ADDR", "DB_PATH", "CORPUS_DIR", "ADMIN_LOGIN", "ADMIN_PASSWORD", "COOKIE_SECURE",
		"LLM_BASE_URL", "LLM_MODEL", "LLM_API_KEY", "LLM_TIMEOUT", "LLM_DISABLE_REASONING", "LLM_VERIFY_ANSWER", "LLM_JSON_MODE", "LLM_MIN_INTERVAL",
	} {
		t.Setenv(name, "")
	}
	if _, err := LoadServer(); err == nil || !strings.Contains(err.Error(), "ADMIN_LOGIN") {
		t.Fatalf("missing admin configuration: %v", err)
	}
	t.Setenv("ADMIN_LOGIN", "admin")
	t.Setenv("ADMIN_PASSWORD", "  raw password  ")
	cfg, err := LoadServer()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:0" || cfg.DBPath != "pdd-assistant.sqlite" || cfg.CorpusDir != "corpus" || cfg.CookieSecure {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.AdminPassword != "  raw password  " || cfg.LLM.Model != "nvidia/nemotron-3.5-super-vl:free" || !cfg.LLM.DisableReasoning || cfg.LLM.Timeout != 120*time.Second || cfg.LLM.VerifyAnswer || !cfg.LLM.JSONMode || cfg.LLM.MinInterval != 4*time.Second {
		t.Fatal("password or LLM defaults changed")
	}
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("LLM_TIMEOUT", "2m")
	t.Setenv("LLM_DISABLE_REASONING", "false")
	cfg, err = LoadServer()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CookieSecure || cfg.LLM.Timeout != 2*time.Minute || cfg.LLM.DisableReasoning {
		t.Fatal("configuration overrides were ignored")
	}
	t.Setenv("LLM_TIMEOUT", "-1s")
	if _, err := LoadLLM(); err == nil || !strings.Contains(err.Error(), "LLM_TIMEOUT") {
		t.Fatalf("negative timeout accepted: %v", err)
	}
}

func TestRejectInvalidAPIControls(t *testing.T) {
	for _, setting := range []struct{ key, value string }{{"LLM_MIN_INTERVAL", "-1s"}, {"LLM_MIN_INTERVAL", "invalid"}, {"LLM_VERIFY_ANSWER", "invalid"}, {"LLM_JSON_MODE", "invalid"}} {
		t.Run(setting.key+setting.value, func(t *testing.T) {
			t.Setenv(setting.key, setting.value)
			if _, err := LoadLLM(); err == nil {
				t.Error("invalid API setting accepted")
			}
		})
	}
}
