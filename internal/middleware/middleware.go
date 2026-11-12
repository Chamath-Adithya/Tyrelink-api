package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solveo/tyrelink/backend/internal/config"
)

type Middleware struct {
	cfg     *config.Config
	limiter *rateLimiter
}

type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func New(cfg *config.Config) *Middleware {
	return &Middleware{
		cfg: cfg,
		limiter: &rateLimiter{
			hits: make(map[string][]time.Time),
		},
	}
}

// CORS middleware supporting preflight and credentials
func (m *Middleware) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := false

		if origin == "" {
			allowed = true
		} else {
			for _, o := range m.cfg.CorsAllowedOrigins {
				if o == "*" || strings.EqualFold(o, origin) {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
			if !allowed {
				// Default allow for local frontend development ports
				if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Authorization, X-Requested-With, Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets hardening headers
func (m *Middleware) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// RateLimit implements in-memory sliding window rate limiting
func (m *Middleware) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Forwarded-For")
		if key == "" {
			key = strings.Split(r.RemoteAddr, ":")[0]
		}
		now := time.Now()
		m.limiter.mu.Lock()
		recent := m.limiter.hits[key][:0]
		for _, t := range m.limiter.hits[key] {
			if now.Sub(t) < time.Minute {
				recent = append(recent, t)
			}
		}
		if len(recent) >= 150 {
			m.limiter.hits[key] = recent
			m.limiter.mu.Unlock()
			JSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please retry in 60s")
			return
		}
		m.limiter.hits[key] = append(recent, now)
		m.limiter.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// Logging logs each request method, path, and duration
func (m *Middleware) Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started))
	})
}

// SignAdminSession generates a secure HMAC SHA256 token valid for 12 hours
func (m *Middleware) SignAdminSession(role string) string {
	exp := time.Now().Add(12 * time.Hour).Unix()
	payload := role + ":" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// RequireAdmin verifies Bearer token against HMAC signature
func (m *Middleware) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		parts := strings.Split(token, ".")
		if len(parts) != 2 || m.cfg.SessionSecret == "" {
			JSONError(w, http.StatusUnauthorized, "admin authorization required")
			return
		}

		raw, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			JSONError(w, http.StatusUnauthorized, "malformed auth token")
			return
		}

		fields := strings.Split(string(raw), ":")
		if len(fields) != 2 || fields[0] != "admin" {
			JSONError(w, http.StatusUnauthorized, "invalid role")
			return
		}

		exp, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || exp < time.Now().Unix() {
			JSONError(w, http.StatusUnauthorized, "token expired")
			return
		}

		mac := hmac.New(sha256.New, []byte(m.cfg.SessionSecret))
		mac.Write(raw)
		sig, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
			JSONError(w, http.StatusUnauthorized, "invalid token signature")
			return
		}

		next(w, r)
	}
}

// JSON helpers
func JSONWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func JSONError(w http.ResponseWriter, status int, msg any) {
	JSONWrite(w, status, map[string]any{"error": msg})
}

