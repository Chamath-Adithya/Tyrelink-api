package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solveo/tyrelink/backend/internal/config"
)

type contextKey string

const (
	actorContextKey contextKey = "tyrelink_admin_actor"
	SessionCookie              = "tyrelink_admin_session"
)

type Middleware struct {
	cfg          *config.Config
	limiter      *rateLimiter
	loginLimiter *rateLimiter
}

type rateLimiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	window  time.Duration
	maxHits int
	ops     int
}

func newRateLimiter(window time.Duration, maxHits int) *rateLimiter {
	return &rateLimiter{
		hits:    make(map[string][]time.Time),
		window:  window,
		maxHits: maxHits,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.ops++
	// Periodically evict empty/expired IP buckets to avoid unbounded growth
	if rl.ops%500 == 0 {
		for k, times := range rl.hits {
			active := false
			for _, t := range times {
				if now.Sub(t) < rl.window {
					active = true
					break
				}
			}
			if !active {
				delete(rl.hits, k)
			}
		}
	}

	recent := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if now.Sub(t) < rl.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= rl.maxHits {
		rl.hits[key] = recent
		return false
	}
	rl.hits[key] = append(recent, now)
	return true
}

func New(cfg *config.Config) *Middleware {
	return &Middleware{
		cfg:          cfg,
		limiter:      newRateLimiter(time.Minute, 180),
		loginLimiter: newRateLimiter(5*time.Minute, 12),
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// ActorFromContext returns the authenticated admin role/actor for auditing
func ActorFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorContextKey).(string); ok && v != "" {
		return v
	}
	return "admin"
}

// CORS middleware supporting preflight and credentials
func (m *Middleware) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			allowed := false
			for _, o := range m.cfg.CorsAllowedOrigins {
				if o == "*" || strings.EqualFold(o, origin) {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
			if !allowed && !strings.EqualFold(m.cfg.Env, "production") {
				if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}
		}

		w.Header().Set("Vary", "Origin")
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

// RateLimit implements bounded sliding-window rate limiting
func (m *Middleware) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.limiter.allow(clientIP(r)) {
			JSONError(w, http.StatusTooManyRequests, "rate limit exceeded, please retry in 60s")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoginRateLimit strictly limits admin authentication attempts against brute force
func (m *Middleware) LoginRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.loginLimiter.allow(clientIP(r)) {
			JSONError(w, http.StatusTooManyRequests, "too many login attempts; please wait 5 minutes")
			return
		}
		next(w, r)
	}
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

func (m *Middleware) SetSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	isSecure := strings.EqualFold(m.cfg.Env, "production") || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   12 * 3600,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Middleware) ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	isSecure := strings.EqualFold(m.cfg.Env, "production") || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func extractAdminToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

// RequireAdmin verifies Bearer token or HttpOnly session cookie against HMAC signature
func (m *Middleware) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractAdminToken(r)
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

		ctx := context.WithValue(r.Context(), actorContextKey, fields[0])
		next(w, r.WithContext(ctx))
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
