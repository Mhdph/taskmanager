package config

import "testing"

func TestStorageTLSConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("HOSTED_GRAPHQL_ENDPOINT", "https://hosted.test/graphql")
	for _, value := range []string{"false", "true", "invalid"} {
		t.Setenv("MINIO_USE_SSL", value)
		cfg, err := Load()
		if value == "invalid" {
			if err == nil {
				t.Fatal("accepted invalid MINIO_USE_SSL")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Storage.UseSSL != (value == "true") {
			t.Fatal("storage TLS setting was ignored")
		}
	}
}
