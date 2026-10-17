package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Environment string

	Server ServerConfig

	Database DatabaseConfig

	Hosted HostedConfig

	Notification NotificationConfig

	Storage StorageConfig
	AI      AIConfig
}
type AIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}
type ServerConfig struct {
	Host string
	Port string
}

type StorageConfig struct {
	Endpoint string

	AccessKey string
	SecretKey string

	Bucket string

	UseSSL    bool
	PublicURL string

	MaxUploadSize int64
}

type NotificationConfig struct {
	BaseURL string
}

type DatabaseConfig struct {
	URL string
}

type HostedConfig struct {
	GraphQLEndpoint string

	// DevMode skips the hosted identity call and derives identities from
	// the USER-ID/APP-ID headers alone (admin role, support flag). Used
	// for local development of clients (e.g. the SvelteKit UI); never
	// enable it on a deployed instance.
	DevMode bool
}

func Load() (Config, error) {
	databaseURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	graphQLEndpoint, err := requireEnv("HOSTED_GRAPHQL_ENDPOINT")
	if err != nil {
		return Config{}, err
	}
	useSSL, err := strconv.ParseBool(getEnv("MINIO_USE_SSL", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("MINIO_USE_SSL must be a boolean")
	}

	cfg := Config{
		AI: AIConfig{
			APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
			BaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
			Model:   getEnv("DEEPSEEK_MODEL", "deepseek-flash"),
		},
		Environment: getEnv("APP_ENV", "development"),

		Server: ServerConfig{
			Host: getEnv("APP_HOST", "0.0.0.0"),
			Port: getEnv("APP_PORT", "8080"),
		},

		Database: DatabaseConfig{
			URL: databaseURL,
		},

		Hosted: HostedConfig{
			GraphQLEndpoint: graphQLEndpoint,

			DevMode: getEnv("IDENTITY_DEV_MODE", "false") == "true",
		},

		Notification: NotificationConfig{
			BaseURL: os.Getenv(
				"NOTIFICATION_SERVICE_URL",
			),
		},
		Storage: StorageConfig{
			Endpoint: os.Getenv(
				"MINIO_ENDPOINT",
			),

			AccessKey: os.Getenv(
				"MINIO_ACCESS_KEY",
			),

			SecretKey: os.Getenv(
				"MINIO_SECRET_KEY",
			),

			Bucket: os.Getenv(
				"MINIO_BUCKET",
			),

			UseSSL:    useSSL,
			PublicURL: os.Getenv("MINIO_PUBLIC_URL"),

			MaxUploadSize: 25 * 1024 * 1024,
		},
	}

	return cfg, nil
}

func (s ServerConfig) Address() string {
	return s.Host + ":" + s.Port
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}

func requireEnv(key string) (string, error) {
	value := os.Getenv(key)

	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}

	return value, nil
}
