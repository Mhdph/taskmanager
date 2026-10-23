package config

import "testing"

func TestDevIdentityEnvironmentGuard(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("HOSTED_GRAPHQL_ENDPOINT", "https://hosted.test/graphql")
	for _, tc := range []struct {
		env, dev string
		allowed  bool
	}{
		{"development", "true", true},
		{"test", "TRUE", true},
		{"production", "false", true},
		{"production", "true", false},
		{" Production ", "1", false},
		{"staging", "true", false},
		{"development", "invalid", false},
	} {
		t.Run(tc.env+"/"+tc.dev, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.env)
			t.Setenv("IDENTITY_DEV_MODE", tc.dev)
			_, err := Load()
			if (err == nil) != tc.allowed {
				t.Fatalf("Load() error = %v, allowed = %v", err, tc.allowed)
			}
		})
	}
}
