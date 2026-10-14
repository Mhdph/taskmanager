package config

import "testing"

func TestDeepSeekConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("HOSTED_GRAPHQL_ENDPOINT", "https://hosted.test/graphql")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DEEPSEEK_BASE_URL", "")
	t.Setenv("DEEPSEEK_MODEL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.APIKey != "" || cfg.AI.BaseURL != "https://api.deepseek.com" || cfg.AI.Model != "deepseek-flash" {
		t.Fatal("unexpected AI defaults")
	}
	t.Setenv("DEEPSEEK_API_KEY", "test-secret")
	t.Setenv("DEEPSEEK_BASE_URL", "https://provider.test/v1")
	t.Setenv("DEEPSEEK_MODEL", "custom-model")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.APIKey != "test-secret" || cfg.AI.BaseURL != "https://provider.test/v1" || cfg.AI.Model != "custom-model" {
		t.Fatal("AI overrides ignored")
	}
}
