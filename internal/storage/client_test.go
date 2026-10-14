package storage

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestPublicDownloadURLUsesExternalHost(t *testing.T) {
	client, err := NewClient("minio:9000", "test-access", "test-secret", "test-bucket", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetPublicURL("https://files.example.test", "test-access", "test-secret"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	signed, err := client.PresignedDownloadURL(ctx, "attachments/test", "test.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "files.example.test" || u.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("signed URL does not use the public endpoint")
	}
	if client.client.EndpointURL().Host != "minio:9000" {
		t.Fatal("public URL changed internal storage connection")
	}
}

func TestPublicDownloadURLRejectsInvalidOrigins(t *testing.T) {
	client, err := NewClient("minio:9000", "test-access", "test-secret", "test-bucket", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"files.test", "ftp://files.test", "https://user:pass@files.test", "https://files.test/prefix", "https://files.test?query=1"} {
		if err := client.SetPublicURL(value, "test-access", "test-secret"); err == nil {
			t.Fatalf("accepted invalid public URL %q", value)
		}
	}
}
