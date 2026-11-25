package config

import (
	"log"
	"os"
	"strings"
)

type Config struct {
	Env                 string
	Port                string
	DatabaseURL         string
	RedisURL            string
	AdminPassword       string
	SessionSecret       string
	CorsAllowedOrigins  []string
	WhatsAppPhoneNumber string
}

func Load() *Config {
	env := getEnv("TYRELINK_ENV", getEnv("APP_ENV", "development"))
	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "")
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")
	adminPass := getEnv("TYRELINK_ADMIN_PASSWORD", "admin")
	sessionSecret := getEnv("TYRELINK_SESSION_SECRET", "tyrelink-secure-session-key-dev-2026")
	cors := getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:4321,http://localhost:4322,http://localhost:3000,http://127.0.0.1:4321,http://127.0.0.1:4322,https://tyrelink.lk")
	wa := getEnv("WHATSAPP_PHONE_NUMBER", "94783724920")

	if strings.EqualFold(env, "production") {
		if adminPass == "admin" || len(adminPass) < 8 {
			log.Fatal("SECURITY ERROR: TYRELINK_ADMIN_PASSWORD must be changed to a strong password (>=8 chars) in production")
		}
		if sessionSecret == "tyrelink-secure-session-key-dev-2026" || len(sessionSecret) < 24 {
			log.Fatal("SECURITY ERROR: TYRELINK_SESSION_SECRET must be set to a unique >=24-char secret in production")
		}
	} else if adminPass == "admin" {
		log.Println("WARNING: Running with default development admin password; override TYRELINK_ADMIN_PASSWORD before deploying")
	}

	var origins []string
	for _, o := range strings.Split(cors, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	return &Config{
		Env:                 env,
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
