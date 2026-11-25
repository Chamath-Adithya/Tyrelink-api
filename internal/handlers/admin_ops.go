package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/solveo/tyrelink/backend/internal/middleware"
	"github.com/solveo/tyrelink/backend/internal/models"
)

// GET /v1/campaigns
func (h *Handlers) GetCampaigns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cacheKey := "tyrelink:campaigns:home"

	var cached models.CampaignConfig
	if h.cache != nil && h.cache.Get(ctx, cacheKey, &cached) {
		middleware.JSONWrite(w, http.StatusOK, cached)
		return
	}

	if !h.db.IsPostgres() {
		cfg := h.db.GetMemoryCampaigns()
		if h.cache != nil {
			_ = h.cache.Set(ctx, cacheKey, cfg, 2*time.Minute)
		}
		middleware.JSONWrite(w, http.StatusOK, cfg)
		return
	}

	var raw []byte
	err := h.db.Pool.QueryRow(ctx, `
		SELECT content FROM campaigns WHERE slug='home-campaigns' LIMIT 1
	`).Scan(&raw)
	if err != nil {
		cfg := h.db.GetMemoryCampaigns()
		middleware.JSONWrite(w, http.StatusOK, cfg)
		return
	}

	var cfg models.CampaignConfig
	if json.Unmarshal(raw, &cfg) != nil {
		cfg = h.db.GetMemoryCampaigns()
	}
	if cfg.FlashSale.ProductIDs == nil {
		cfg.FlashSale.ProductIDs = []string{}
	}
	if cfg.Trending.ProductIDs == nil {
		cfg.Trending.ProductIDs = []string{}
	}
	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, cfg, 2*time.Minute)
	}
	middleware.JSONWrite(w, http.StatusOK, cfg)
}

// PUT /v1/admin/campaigns
func (h *Handlers) SaveCampaigns(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var cfg models.CampaignConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64000)).Decode(&cfg); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid campaigns payload")
		return
	}

	if cfg.FlashSale.ProductIDs == nil {
		cfg.FlashSale.ProductIDs = []string{}
	}
	if cfg.Trending.ProductIDs == nil {
		cfg.Trending.ProductIDs = []string{}
	}
	if cfg.FlashSale.Title == "" {
		cfg.FlashSale.Title = "Today's Flash Deals"
	}
	if cfg.Trending.Title == "" {
		cfg.Trending.Title = "Trending & Best Sellers"
	}

	h.db.SaveMemoryCampaigns(cfg)

	if h.db.IsPostgres() {
		ctx := r.Context()
		contentJSON, _ := json.Marshal(cfg)

		var startsAt, endsAt *time.Time
		if t, err := time.Parse(time.RFC3339, cfg.FlashSale.StartTime); err == nil {
			startsAt = &t
		}
		if t, err := time.Parse(time.RFC3339, cfg.FlashSale.EndTime); err == nil {
			endsAt = &t
		}

		tx, err := h.db.Pool.Begin(ctx)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "could not update campaigns")
			return
		}
		defer tx.Rollback(ctx)

		_, err = tx.Exec(ctx, `
			INSERT INTO campaigns (title, slug, content, active, starts_at, ends_at)
			VALUES ('Homepage Campaigns', 'home-campaigns', $1, $2, $3, $4)
			ON CONFLICT (slug) DO UPDATE SET
				content = EXCLUDED.content,
				active = EXCLUDED.active,
				starts_at = EXCLUDED.starts_at,
				ends_at = EXCLUDED.ends_at
		`, contentJSON, cfg.FlashSale.IsActive || cfg.Trending.IsActive, startsAt, endsAt)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "failed saving campaign config")
			return
		}

		_, err = tx.Exec(ctx, `UPDATE products SET is_flash_deal = (id = ANY($1::text[])), is_best_seller = (id = ANY($2::text[])), updated_at = now()`,
			cfg.FlashSale.ProductIDs, cfg.Trending.ProductIDs)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "failed syncing product flags")
			return
		}

		if tx.Commit(ctx) != nil {
			middleware.JSONError(w, http.StatusInternalServerError, "campaign update commit failed")
			return
		}
	}

	if h.cache != nil {
		_ = h.cache.Delete(r.Context(), "tyrelink:campaigns:home", "tyrelink:admin:stats")
	}
	h.flushProductCache(r.Context())
	h.recordAudit(r.Context(), "campaigns.update", "campaign", "home-campaigns", map[string]any{
		"flashActive": cfg.FlashSale.IsActive,
		"flashCount":  len(cfg.FlashSale.ProductIDs),
		"trendCount":  len(cfg.Trending.ProductIDs),
	})
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "campaigns": cfg})
}

// GET /v1/admin/stats
func (h *Handlers) GetAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := models.AdminStats{
		FlatDeliveryFee:   450,
		PostgresConnected: h.db != nil && h.db.IsPostgres(),
		RedisConnected:    h.cache != nil && h.cache.IsRedis(),
	}

	if !stats.PostgresConnected {
		prods := h.db.GetAllMemoryProducts()
		stats.TotalProducts = len(prods)
		for _, p := range prods {
			if p.Published {
				stats.PublishedProducts++
			}
			if !p.InStock || p.StockCount <= 0 {
				stats.OutOfStockProducts++
			}
			if p.IsFlashDeal {
				stats.FlashDealCount++
			}
			if p.IsBestSeller {
				stats.BestSellerCount++
			}
		}
		leads := h.db.GetMemoryLeads()
		stats.TotalLeads = len(leads)
		for _, l := range leads {
			switch l.Status {
			case "new":
				stats.NewLeads++
			case "contacted", "qualified":
				stats.ContactedLeads++
			case "dispatched":
				stats.DispatchedLeads++
			case "delivered":
				stats.DeliveredLeads++
			}
			if l.Status != "spam" && l.Status != "cancelled" {
				stats.LeadsPipelineLKR += l.GrandTotal
			}
		}
		middleware.JSONWrite(w, http.StatusOK, stats)
		return
	}

	_ = h.db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE published=true)::int,
			COUNT(*) FILTER (WHERE in_stock=false OR stock_count<=0)::int,
			COUNT(*) FILTER (WHERE is_flash_deal=true)::int,
			COUNT(*) FILTER (WHERE is_best_seller=true)::int
		FROM products
	`).Scan(&stats.TotalProducts, &stats.PublishedProducts, &stats.OutOfStockProducts, &stats.FlashDealCount, &stats.BestSellerCount)

	_ = h.db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status='new')::int,
			COUNT(*) FILTER (WHERE status IN ('contacted','qualified'))::int,
			COUNT(*) FILTER (WHERE status='dispatched')::int,
			COUNT(*) FILTER (WHERE status='delivered')::int,
			COALESCE(SUM(grand_total_minor) FILTER (WHERE status NOT IN ('spam','cancelled')), 0)::bigint / 100
		FROM order_leads
	`).Scan(&stats.TotalLeads, &stats.NewLeads, &stats.ContactedLeads, &stats.DispatchedLeads, &stats.DeliveredLeads, &stats.LeadsPipelineLKR)

	_ = h.db.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status='pending')::int,
			COUNT(*) FILTER (WHERE status='approved')::int
		FROM product_reviews
	`).Scan(&stats.PendingReviews, &stats.ApprovedReviews)

	var fee int
	if h.db.Pool.QueryRow(ctx, `SELECT fee FROM delivery_rates ORDER BY (town='Islandwide') DESC, town LIMIT 1`).Scan(&fee) == nil {
		stats.FlatDeliveryFee = fee
	}

	middleware.JSONWrite(w, http.StatusOK, stats)
}

// GET /v1/admin/audit-logs
func (h *Handlers) GetAdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !h.db.IsPostgres() {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": []models.AuditLog{}})
		return
	}

	rows, err := h.db.Pool.Query(r.Context(), `
		SELECT id, actor, action, entity_type, entity_id, coalesce(details, '{}'::jsonb), created_at
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": []models.AuditLog{}})
		return
	}
	defer rows.Close()

	logs := []models.AuditLog{}
	for rows.Next() {
		var entry models.AuditLog
		var detailsRaw []byte
		if rows.Scan(&entry.ID, &entry.Actor, &entry.Action, &entry.EntityType, &entry.EntityID, &detailsRaw, &entry.CreatedAt) == nil {
			_ = json.Unmarshal(detailsRaw, &entry.Details)
			logs = append(logs, entry)
		}
	}
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": logs})
}
