package config

import (
	"fmt"
	"os"
)

type Config struct {
	Environment string

	Server ServerConfig

	Database DatabaseConfig

	Hosted HostedConfig

	Notification NotificationConfig
}
type ServerConfig struct {
	Host string
	Port string
}

type NotificationConfig struct {
	BaseURL string
}

type DatabaseConfig struct {
	URL string
}

type HostedConfig struct {
	GraphQLEndpoint string
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

	cfg := Config{
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
		},

		Notification: NotificationConfig{
			BaseURL: os.Getenv(
				"NOTIFICATION_SERVICE_URL",
			),
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
