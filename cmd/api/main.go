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

	log.Printf("Starting Tyrelink Enterprise API on port :%s", cfg.Port)

	// 1. Connect Database (Postgres with connection pool or memory fallback)
	db := database.Connect(ctx, cfg.DatabaseURL)
	defer db.Close()

	// 2. Connect Cache (Redis or in-memory fallback)
	redisCache := cache.New(cfg.RedisURL)

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
	mux.HandleFunc("POST /v1/order-leads", h.CreateOrderLead)

	// Admin Auth
	mux.HandleFunc("POST /v1/admin/login", h.AdminLogin)
	mux.HandleFunc("GET /v1/admin/verify", mw.RequireAdmin(h.AdminVerify))

	// Protected Admin Product Management
	mux.HandleFunc("POST /v1/admin/products", mw.RequireAdmin(h.AdminCreateProduct))
	mux.HandleFunc("PUT /v1/admin/products/{id}", mw.RequireAdmin(h.AdminUpdateProduct))
	mux.HandleFunc("DELETE /v1/admin/products/{id}", mw.RequireAdmin(h.AdminDeleteProduct))

	// Protected Admin Order Leads & CRM
	mux.HandleFunc("GET /v1/admin/leads", mw.RequireAdmin(h.GetAdminLeads))

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
