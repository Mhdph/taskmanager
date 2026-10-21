package config

import (
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://localhost/taskmanager")
	t.Setenv("HOSTED_GRAPHQL_ENDPOINT", "https://identity.example/graphql")
}

func TestAutomationConfigDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Automation.SweepInterval != 15*time.Minute {
		t.Fatalf(
			"unexpected sweep interval %s",
			cfg.Automation.SweepInterval,
		)
	}

	if cfg.Automation.MaxRulesPerProject != 20 {
		t.Fatalf(
			"unexpected rule limit %d",
			cfg.Automation.MaxRulesPerProject,
		)
	}
}

func TestAutomationConfigOverrides(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("AUTOMATION_SWEEP_INTERVAL", "5m")
	t.Setenv("AUTOMATION_MAX_RULES_PER_PROJECT", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Automation.SweepInterval != 5*time.Minute {
		t.Fatalf(
			"unexpected sweep interval %s",
			cfg.Automation.SweepInterval,
		)
	}

	if cfg.Automation.MaxRulesPerProject != 5 {
		t.Fatalf(
			"unexpected rule limit %d",
			cfg.Automation.MaxRulesPerProject,
		)
	}
}

func TestAutomationConfigRejectsInvalidValues(t *testing.T) {
	tests := map[string]map[string]string{
		"sweep interval": {
			"AUTOMATION_SWEEP_INTERVAL": "soon",
		},
		"rule limit": {
			"AUTOMATION_MAX_RULES_PER_PROJECT": "0",
		},
		"rule limit too high": {
			"AUTOMATION_MAX_RULES_PER_PROJECT": "101",
		},
	}

	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			setRequiredEnv(t)

			for key, value := range env {
				t.Setenv(key, value)
			}

			if _, err := Load(); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}
