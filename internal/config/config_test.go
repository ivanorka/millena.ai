package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("STATIC_DIR", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("DATABASE_MAX_CONNECTIONS", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("DEMO_ADMIN_EMAIL", "")
	t.Setenv("DEMO_ADMIN_NAME", "")
	t.Setenv("DEMO_ADMIN_PASSWORD", "")
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("AI_REQUEST_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if cfg.Address() != ":8080" {
		t.Fatalf("expected :8080, got %q", cfg.Address())
	}
	if cfg.DatabaseMaxConnections != 10 {
		t.Fatalf("expected 10 database connections, got %d", cfg.DatabaseMaxConnections)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("expected 2 default CORS origins, got %d", len(cfg.CORSAllowedOrigins))
	}
	if cfg.SessionTTL != 30*24*time.Hour {
		t.Fatalf("expected 30 day session TTL, got %s", cfg.SessionTTL)
	}
	if cfg.AIProvider != "local" || cfg.AIRequestTimeout != 90*time.Second {
		t.Fatalf("expected local AI defaults, got provider=%q timeout=%s", cfg.AIProvider, cfg.AIRequestTimeout)
	}
}

func TestLoadRejectsInvalidConnectionCount(t *testing.T) {
	t.Setenv("DATABASE_MAX_CONNECTIONS", "zero")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid DATABASE_MAX_CONNECTIONS to fail")
	}
}
