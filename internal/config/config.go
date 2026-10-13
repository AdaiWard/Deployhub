package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	DatabaseURL         string
	RedisAddr           string
	RedisPassword       string
	EncryptionKey       string
	SessionSecret       string
	ExternalURL         string
	BaseDomain          string
	URLScheme           string
	DockerNetwork       string
	TraefikEntryPoint   string
	TraefikCertResolver string
	GitHubClientID      string
	GitHubClientSecret  string
	GitHubWebhookSecret string
	DevMode             bool
	AllowedOrigins      []string
	WorkerConcurrency   int
	DeployTimeout       time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:            env("HTTP_ADDR", ":8080"),
		DatabaseURL:         env("DATABASE_URL", "postgres://deployhub:deployhub@localhost:5432/deployhub?sslmode=disable"),
		RedisAddr:           env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		EncryptionKey:       env("ENCRYPTION_KEY", "deployhub-local-development-key-change-me"),
		SessionSecret:       env("SESSION_SECRET", "deployhub-local-session-secret-change-me"),
		ExternalURL:         strings.TrimRight(env("EXTERNAL_URL", "http://deployhub.localhost"), "/"),
		BaseDomain:          env("BASE_DOMAIN", "localhost"),
		URLScheme:           env("URL_SCHEME", "http"),
		DockerNetwork:       env("DOCKER_NETWORK", "deployhub_runtime"),
		TraefikEntryPoint:   env("TRAEFIK_ENTRYPOINT", "web"),
		TraefikCertResolver: os.Getenv("TRAEFIK_CERT_RESOLVER"),
		GitHubClientID:      os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
		DevMode:             envBool("DEV_MODE", true),
		AllowedOrigins:      splitCSV(os.Getenv("ALLOWED_ORIGINS")),
		WorkerConcurrency:   envInt("WORKER_CONCURRENCY", 2),
		DeployTimeout:       time.Duration(envInt("DEPLOY_TIMEOUT_MINUTES", 20)) * time.Minute,
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
