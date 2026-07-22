package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDatabaseURL = "postgres://millena:millena@127.0.0.1:5432/millena?sslmode=disable"

type Config struct {
	Environment            string
	Port                   string
	DatabaseURL            string
	StaticDir              string
	CORSAllowedOrigins     []string
	DatabaseMaxConnections int32
	ShutdownTimeout        time.Duration
	SessionTTL             time.Duration
	DemoAdminEmail         string
	DemoAdminName          string
	DemoAdminPassword      string
	AIProvider             string
	OllamaBaseURL          string
	OllamaModel            string
	AIRequestTimeout       time.Duration
}

func Load() (Config, error) {
	maxConnections, err := strconv.ParseInt(envOrDefault("DATABASE_MAX_CONNECTIONS", "10"), 10, 32)
	if err != nil || maxConnections < 1 {
		return Config{}, fmt.Errorf("DATABASE_MAX_CONNECTIONS must be a positive integer")
	}

	shutdownTimeout, err := time.ParseDuration(envOrDefault("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || shutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be a positive duration")
	}
	sessionTTL, err := time.ParseDuration(envOrDefault("SESSION_TTL", "720h"))
	if err != nil || sessionTTL < time.Hour {
		return Config{}, fmt.Errorf("SESSION_TTL must be a duration of at least one hour")
	}
	demoPassword := envOrDefault("DEMO_ADMIN_PASSWORD", "irena123")
	if len(demoPassword) < 8 {
		return Config{}, fmt.Errorf("DEMO_ADMIN_PASSWORD must contain at least 8 characters")
	}
	aiProvider := strings.ToLower(envOrDefault("AI_PROVIDER", "local"))
	if aiProvider != "local" && aiProvider != "ollama" {
		return Config{}, fmt.Errorf("AI_PROVIDER must be local or ollama")
	}
	ollamaModel := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
	if aiProvider == "ollama" && ollamaModel == "" {
		return Config{}, fmt.Errorf("OLLAMA_MODEL is required when AI_PROVIDER=ollama")
	}
	aiRequestTimeout, err := time.ParseDuration(envOrDefault("AI_REQUEST_TIMEOUT", "90s"))
	if err != nil || aiRequestTimeout < time.Second {
		return Config{}, fmt.Errorf("AI_REQUEST_TIMEOUT must be a duration of at least one second")
	}

	return Config{
		Environment:            envOrDefault("APP_ENV", "development"),
		Port:                   envOrDefault("PORT", "8080"),
		DatabaseURL:            envOrDefault("DATABASE_URL", defaultDatabaseURL),
		StaticDir:              envOrDefault("STATIC_DIR", "."),
		CORSAllowedOrigins:     splitCSV(envOrDefault("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:8000,http://localhost:8000")),
		DatabaseMaxConnections: int32(maxConnections),
		ShutdownTimeout:        shutdownTimeout,
		SessionTTL:             sessionTTL,
		DemoAdminEmail:         strings.ToLower(envOrDefault("DEMO_ADMIN_EMAIL", "irena@mpr.hr")),
		DemoAdminName:          envOrDefault("DEMO_ADMIN_NAME", "Irena"),
		DemoAdminPassword:      demoPassword,
		AIProvider:             aiProvider,
		OllamaBaseURL:          envOrDefault("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		OllamaModel:            ollamaModel,
		AIRequestTimeout:       aiRequestTimeout,
	}, nil
}

func (c Config) Address() string {
	return ":" + c.Port
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
