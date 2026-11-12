package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct{ db *pgxpool.Pool }
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

type product struct {
	ID              string `json:"id"`
	SKU             string `json:"sku"`
	Brand           string `json:"brand"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	Size            string `json:"size"`
	PriceMinor      int64  `json:"priceMinor"`
	Currency        string `json:"currency"`
	PriceIndicative bool   `json:"priceIndicative"`
}

func main() {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	var db *pgxpool.Pool
	if dsn != "" {
		var err error
		db, err = pgxpool.New(ctx, dsn)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
	}
	s := &server{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/products", s.products)
	mux.HandleFunc("GET /v1/products/{id}", s.productByID)
	mux.HandleFunc("POST /v1/order-leads", s.createLead)
	mux.HandleFunc("POST /v1/order-leads/{id}/whatsapp-link", s.whatsappLink)
	mux.HandleFunc("GET /v1/warranty/{invoice}", s.warranty)
	mux.HandleFunc("GET /v1/orders/{id}", s.order)
	mux.HandleFunc("POST /v1/admin/products", s.adminCreateProduct)
	mux.HandleFunc("PUT /v1/admin/products/{id}", s.adminUpdateProduct)
	mux.HandleFunc("DELETE /v1/admin/products/{id}", s.adminDeleteProduct)
	mux.HandleFunc("GET /v1/admin/leads", s.adminLeads)
	mux.HandleFunc("POST /v1/admin/login", s.adminLogin)
	addr := ":" + getenv("PORT", "8080")
	log.Printf("Tyrelink API listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, securityHeaders(rateLimit(logging(mux)))))
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	jsonWrite(w, http.StatusOK, map[string]string{"status": "ok", "service": "tyrelink-api"})
}

func (s *server) products(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		jsonWrite(w, http.StatusOK, map[string]any{"data": []product{{"demo-ceat-zoom-plus", "TL-MOTO-001", "CEAT", "Zoom Plus", "motorcycle", "90/90-17", 1290000, "LKR", true}, {"demo-dsi-s-drive", "TL-CAR-001", "DSI", "S-Drive", "car", "185/65 R15", 2990000, "LKR", true}}})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT id, sku, brand, name, category, size, price_minor, currency, price_indicative FROM products WHERE published=true ORDER BY created_at DESC`)
	if err != nil {
		jsonError(w, 500, err)
		return
	}
	defer rows.Close()
	items := []product{}
	for rows.Next() {
		var p product
		if err := rows.Scan(&p.ID, &p.SKU, &p.Brand, &p.Name, &p.Category, &p.Size, &p.PriceMinor, &p.Currency, &p.PriceIndicative); err != nil {
			jsonError(w, 500, err)
			return
		}
		items = append(items, p)
	}
	jsonWrite(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) productByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	jsonWrite(w, http.StatusOK, map[string]any{"id": id, "message": "Product detail endpoint ready"})
}

func (s *server) createLead(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		jsonError(w, 400, "request body required")
		return
	}
	defer r.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, 400, "invalid JSON")
		return
	}
	leadID := "lead_" + time.Now().UTC().Format("20060102150405")
	if s.db != nil {
		_, err := s.db.Exec(r.Context(), `INSERT INTO order_leads(id,payload) VALUES($1,$2)`, leadID, payload)
		if err != nil {
			jsonError(w, 500, err)
			return
		}
	}
	jsonWrite(w, 201, map[string]any{"leadId": leadID, "createdAt": time.Now().UTC()})
}

func (s *server) whatsappLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	phone := getenv("WHATSAPP_PHONE_NUMBER", "94783724920")
	message := `Hello Tyrelink, I want help with enquiry ` + id + `. Please confirm fitment, current price and delivery.`
	jsonWrite(w, 200, map[string]string{"url": "https://wa.me/" + phone + "?text=" + urlEncode(message)})
}

func (s *server) warranty(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		jsonWrite(w, http.StatusOK, map[string]any{"invoice": r.PathValue("invoice"), "status": "verification_required", "message": "A Tyrelink team member will verify this invoice."})
		return
	}
	var status string
	var starts, ends time.Time
	err := s.db.QueryRow(r.Context(), `SELECT status, starts_at, ends_at FROM warranty_records WHERE invoice_number=$1`, r.PathValue("invoice")).Scan(&status, &starts, &ends)
	if err != nil {
		jsonWrite(w, http.StatusNotFound, map[string]any{"invoice": r.PathValue("invoice"), "status": "not_found"})
		return
	}
	jsonWrite(w, 200, map[string]any{"invoice": r.PathValue("invoice"), "status": status, "startsAt": starts, "endsAt": ends})
}

func (s *server) order(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		jsonWrite(w, http.StatusOK, map[string]any{"reference": r.PathValue("id"), "status": "enquiry_received", "steps": []string{"Enquiry received", "Fitment checked", "Delivery confirmed", "Completed"}})
		return
	}
	var status, note string
	err := s.db.QueryRow(r.Context(), `SELECT status, COALESCE(tracking_note,'') FROM orders WHERE id=$1`, r.PathValue("id")).Scan(&status, &note)
	if err != nil {
		jsonError(w, 404, "order not found")
		return
	}
	jsonWrite(w, 200, map[string]any{"reference": r.PathValue("id"), "status": status, "note": note, "steps": []string{"Enquiry received", "Fitment checked", "Delivery confirmed", "Completed"}})
}

func (s *server) adminCreateProduct(w http.ResponseWriter, r *http.Request) {
	if !adminSessionOK(r) {
		jsonError(w, http.StatusUnauthorized, "admin authorization required")
		return
	}
	var p product
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, 400, "invalid product JSON")
		return
	}
	if p.ID == "" || p.SKU == "" || p.Brand == "" || p.Name == "" {
		jsonError(w, 400, "id, sku, brand and name are required")
		return
	}
	if s.db == nil {
		jsonWrite(w, 201, map[string]any{"product": p, "persisted": false, "message": "database not configured"})
		return
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO products (id,sku,brand,name,category,size,price_minor,currency,price_indicative,published) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.ID, p.SKU, p.Brand, p.Name, p.Category, p.Size, p.PriceMinor, p.Currency, p.PriceIndicative, true)
	if err != nil {
		jsonError(w, 500, err)
		return
	}
	jsonWrite(w, 201, map[string]any{"product": p, "persisted": true})
}

func (s *server) adminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	if !adminSessionOK(r) {
		jsonError(w, 401, "admin authorization required")
		return
	}
	var p product
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, 400, "invalid product JSON")
		return
	}
	if s.db == nil {
		jsonWrite(w, 200, map[string]any{"updated": false, "message": "database not configured"})
		return
	}
	_, err := s.db.Exec(r.Context(), `UPDATE products SET brand=$1,name=$2,category=$3,size=$4,price_minor=$5,price_indicative=$6,published=$7,updated_at=now() WHERE id=$8`, p.Brand, p.Name, p.Category, p.Size, p.PriceMinor, p.PriceIndicative, true, r.PathValue("id"))
	if err != nil {
		jsonError(w, 500, err)
		return
	}
	jsonWrite(w, 200, map[string]any{"updated": true, "id": r.PathValue("id")})
}
func (s *server) adminDeleteProduct(w http.ResponseWriter, r *http.Request) {
	if !adminSessionOK(r) {
		jsonError(w, 401, "admin authorization required")
		return
	}
	if s.db == nil {
		jsonWrite(w, 200, map[string]any{"deleted": false, "message": "database not configured"})
		return
	}
	_, err := s.db.Exec(r.Context(), `UPDATE products SET published=false,updated_at=now() WHERE id=$1`, r.PathValue("id"))
	if err != nil {
		jsonError(w, 500, err)
		return
	}
	jsonWrite(w, 200, map[string]any{"deleted": true, "id": r.PathValue("id")})
}
func (s *server) adminLeads(w http.ResponseWriter, r *http.Request) {
	if !adminSessionOK(r) {
		jsonError(w, 401, "admin authorization required")
		return
	}
	if s.db == nil {
		jsonWrite(w, 200, map[string]any{"data": []any{}})
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT id,status,created_at FROM order_leads ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		jsonError(w, 500, err)
		return
	}
	defer rows.Close()
	type lead struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"createdAt"`
	}
	items := []lead{}
	for rows.Next() {
		var l lead
		if err := rows.Scan(&l.ID, &l.Status, &l.CreatedAt); err != nil {
			jsonError(w, 500, err)
			return
		}
		items = append(items, l)
	}
	jsonWrite(w, 200, map[string]any{"data": items})
}
func (s *server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	defer r.Body.Close()
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password == "" || body.Password != os.Getenv("TYRELINK_ADMIN_PASSWORD") || os.Getenv("TYRELINK_ADMIN_PASSWORD") == "" {
		jsonError(w, 401, "invalid credentials")
		return
	}
	jsonWrite(w, 200, map[string]string{"token": signSession("admin")})
}
func signSession(role string) string {
	exp := time.Now().Add(8 * time.Hour).Unix()
	payload := role + ":" + strconv.FormatInt(exp, 10)
	secret := os.Getenv("TYRELINK_SESSION_SECRET")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func adminSessionOK(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	secret := os.Getenv("TYRELINK_SESSION_SECRET")
	if secret == "" || len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	fields := strings.Split(string(raw), ":")
	if len(fields) != 2 || fields[0] != "admin" {
		return false
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	return err == nil && hmac.Equal(sig, mac.Sum(nil))
}
func rateLimit(next http.Handler) http.Handler {
	l := &limiter{hits: map[string][]time.Time{}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Forwarded-For")
		if key == "" {
			key = strings.Split(r.RemoteAddr, ":")[0]
		}
		now := time.Now()
		l.mu.Lock()
		recent := l.hits[key][:0]
		for _, t := range l.hits[key] {
			if now.Sub(t) < time.Minute {
				recent = append(recent, t)
			}
		}
		if len(recent) >= 120 {
			l.hits[key] = recent
			l.mu.Unlock()
			jsonError(w, 429, "rate limit exceeded")
			return
		}
		l.hits[key] = append(recent, now)
		l.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started))
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
func jsonWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, status int, msg any) {
	jsonWrite(w, status, map[string]any{"error": msg})
}
func getenv(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func urlEncode(v string) string {
	return strings.NewReplacer(" ", "%20", ",", "%2C", "`", "%60", "'", "%27").Replace(v)
}
