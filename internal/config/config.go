package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment string

	Server ServerConfig

	Database DatabaseConfig

	Hosted HostedConfig

	Notification NotificationConfig

	Storage StorageConfig
	AI      AIConfig

	Chat ChatConfig

	Automation AutomationConfig
}

type AutomationConfig struct {
	// SweepInterval is how often due-date rules are evaluated
	// (AUTOMATION_SWEEP_INTERVAL, default 15m).
	SweepInterval time.Duration

	// MaxRulesPerProject caps stored rules per project
	// (AUTOMATION_MAX_RULES_PER_PROJECT, default 20).
	MaxRulesPerProject int
}

type ChatConfig struct {
	// AllowedOrigins lists cross-origin hosts accepted by the WebSocket
	// upgrade (CHAT_WS_ORIGINS, comma separated). The request host is
	// always accepted.
	AllowedOrigins []string
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

	// ReconcileGracePeriod is how long a storage object must be untouched
	// before the storage reconciler may remove it. It protects uploads
	// that have not reached the database yet.
	ReconcileGracePeriod time.Duration
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
	environment := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "development")))
	devMode, err := strconv.ParseBool(getEnv("IDENTITY_DEV_MODE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("IDENTITY_DEV_MODE must be a boolean")
	}
	if devMode && environment != "development" && environment != "test" {
		return Config{}, fmt.Errorf("IDENTITY_DEV_MODE is only allowed in development or test")
	}
	databaseURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}

	// Objects are only removed once they are old enough that no upload
	// can still be in flight.
	reconcileGracePeriod := 10 * time.Minute

	if raw := os.Getenv("MINIO_RECONCILE_GRACE_PERIOD"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			return Config{}, fmt.Errorf(
				"invalid MINIO_RECONCILE_GRACE_PERIOD: %q",
				raw,
			)
		}

		reconcileGracePeriod = parsed
	}

	automationSweepInterval := 15 * time.Minute

	if raw := os.Getenv("AUTOMATION_SWEEP_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf(
				"invalid AUTOMATION_SWEEP_INTERVAL: %q",
				raw,
			)
		}

		automationSweepInterval = parsed
	}

	automationMaxRules := 20

	if raw := os.Getenv("AUTOMATION_MAX_RULES_PER_PROJECT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return Config{}, fmt.Errorf(
				"AUTOMATION_MAX_RULES_PER_PROJECT must be between 1 and 100",
			)
		}

		automationMaxRules = parsed
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
		Environment: environment,

		Server: ServerConfig{
			Host: getEnv("APP_HOST", "0.0.0.0"),
			Port: getEnv("APP_PORT", "8080"),
		},

		Database: DatabaseConfig{
			URL: databaseURL,
		},

		Hosted: HostedConfig{
			GraphQLEndpoint: graphQLEndpoint,

			DevMode: devMode,
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

			ReconcileGracePeriod: reconcileGracePeriod,
		},

		Chat: ChatConfig{
			AllowedOrigins: splitList(
				os.Getenv("CHAT_WS_ORIGINS"),
			),
		},

		Automation: AutomationConfig{
			SweepInterval:      automationSweepInterval,
			MaxRulesPerProject: automationMaxRules,
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

func splitList(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")

	items := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		items = append(items, part)
	}

	return items
}
