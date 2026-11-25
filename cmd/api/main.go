package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/solveo/tyrelink/backend/internal/cache"
	"github.com/solveo/tyrelink/backend/internal/config"
	"github.com/solveo/tyrelink/backend/internal/database"
	"github.com/solveo/tyrelink/backend/internal/handlers"
	"github.com/solveo/tyrelink/backend/internal/middleware"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Starting Tyrelink Enterprise API on port :%s (env=%s)", cfg.Port, cfg.Env)

	// 1. Connect Database (Postgres with connection pool or memory fallback)
	db := database.Connect(ctx, cfg.DatabaseURL)
	defer db.Close()
	if cfg.DatabaseURL != "" && !db.IsPostgres() {
		log.Fatal("Configured database unavailable; exiting so the service can retry instead of serving a permanent memory fallback")
	}

	// 2. Connect Cache (Redis or in-memory fallback)
	redisCache := cache.New(cfg.RedisURL)
	defer func() { _ = redisCache.Close() }()

	// 3. Initialize Middleware
	mw := middleware.New(cfg)

	// 4. Initialize Handlers
	h := handlers.New(cfg, db, redisCache, mw)

	// 5. Setup Router
	mux := http.NewServeMux()

	// Public Endpoints
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /v1/products", h.GetProducts)
	mux.HandleFunc("GET /v1/products/{id}", h.GetProductByID)
	mux.HandleFunc("GET /v1/products/{id}/reviews", h.GetReviews)
	mux.HandleFunc("POST /v1/products/{id}/reviews", h.CreateReview)
	mux.HandleFunc("POST /v1/order-leads", h.CreateOrderLead)
	mux.HandleFunc("GET /v1/delivery-rates", h.GetDeliveryRates)
	mux.HandleFunc("GET /v1/campaigns", h.GetCampaigns)

	// Admin Auth (with strict brute-force rate limit on login)
	mux.HandleFunc("POST /v1/admin/login", mw.LoginRateLimit(h.AdminLogin))
	mux.HandleFunc("POST /v1/admin/logout", h.AdminLogout)
	mux.HandleFunc("GET /v1/admin/verify", mw.RequireAdmin(h.AdminVerify))

	// Protected Admin Dashboard, Stats & Audit Trail
	mux.HandleFunc("GET /v1/admin/stats", mw.RequireAdmin(h.GetAdminStats))
	mux.HandleFunc("GET /v1/admin/audit-logs", mw.RequireAdmin(h.GetAdminAuditLogs))

	// Protected Admin Product & Catalog Management
	mux.HandleFunc("GET /v1/admin/catalog", mw.RequireAdmin(h.AdminCatalog))
	mux.HandleFunc("PUT /v1/admin/catalog", mw.RequireAdmin(h.AdminBulkCatalog))
	mux.HandleFunc("POST /v1/admin/products", mw.RequireAdmin(h.AdminCreateProduct))
	mux.HandleFunc("PUT /v1/admin/products/{id}", mw.RequireAdmin(h.AdminUpdateProduct))
	mux.HandleFunc("DELETE /v1/admin/products/{id}", mw.RequireAdmin(h.AdminDeleteProduct))

	// Protected Admin Order Leads & CRM
	mux.HandleFunc("GET /v1/admin/leads", mw.RequireAdmin(h.GetAdminLeads))
	mux.HandleFunc("PUT /v1/admin/leads/{id}", mw.RequireAdmin(h.UpdateAdminLead))
	mux.HandleFunc("DELETE /v1/admin/leads/{id}", mw.RequireAdmin(h.DeleteAdminLead))

	// Protected Admin Campaigns (Flash Deals & Trending)
	mux.HandleFunc("PUT /v1/admin/campaigns", mw.RequireAdmin(h.SaveCampaigns))

	// Protected Admin Reviews & Flat Delivery Rate
	mux.HandleFunc("GET /v1/admin/reviews", mw.RequireAdmin(h.GetReviews))
	mux.HandleFunc("PUT /v1/admin/reviews/{id}", mw.RequireAdmin(h.ModerateReview))
	mux.HandleFunc("DELETE /v1/admin/reviews/{id}", mw.RequireAdmin(h.DeleteReview))
	mux.HandleFunc("PUT /v1/admin/delivery-rates", mw.RequireAdmin(h.SaveDeliveryRate))

	// Chain Middlewares: CORS -> Security Headers -> Rate Limiter -> Request Logging
	handler := mw.CORS(mw.SecurityHeaders(mw.RateLimit(mw.Logging(mux))))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown handling
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Tyrelink API listening at http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-shutdownCh
	log.Println("Shutting down Tyrelink API gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Tyrelink API server exited cleanly")
}
