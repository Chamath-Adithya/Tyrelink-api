package config

import (
	"os"
	"strings"
)

type Config struct {
	Port                string
	DatabaseURL         string
	RedisURL            string
	AdminPassword       string
	SessionSecret       string
	CorsAllowedOrigins  []string
	WhatsAppPhoneNumber string
}

func Load() *Config {
	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "")
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")
	adminPass := getEnv("TYRELINK_ADMIN_PASSWORD", "admin")
	sessionSecret := getEnv("TYRELINK_SESSION_SECRET", "tyrelink-secure-session-key-dev-2026")
	cors := getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:4321,http://localhost:4322,http://localhost:3000,http://127.0.0.1:4321,http://127.0.0.1:4322,https://tyrelink.lk")
	wa := getEnv("WHATSAPP_PHONE_NUMBER", "94783724920")

	var origins []string
	for _, o := range strings.Split(cors, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	return &Config{
		Port:                port,
		DatabaseURL:         dbURL,
		RedisURL:            redisURL,
		AdminPassword:       adminPass,
		SessionSecret:       sessionSecret,
		CorsAllowedOrigins:  origins,
		WhatsAppPhoneNumber: wa,
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

