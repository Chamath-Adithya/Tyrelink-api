package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/solveo/tyrelink/backend/internal/cache"
	"github.com/solveo/tyrelink/backend/internal/config"
	"github.com/solveo/tyrelink/backend/internal/database"
	"github.com/solveo/tyrelink/backend/internal/middleware"
	"github.com/solveo/tyrelink/backend/internal/models"
)

var slugCleanRe = regexp.MustCompile(`[^a-z0-9-]+`)

type Handlers struct {
	cfg   *config.Config
	db    *database.DB
	cache cache.Cache
	mw    *middleware.Middleware
}

func New(cfg *config.Config, db *database.DB, c cache.Cache, mw *middleware.Middleware) *Handlers {
	return &Handlers{
		cfg:   cfg,
		db:    db,
		cache: c,
		mw:    mw,
	}
}

func randomHexID(prefix string, bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	if prefix == "" {
		return hex.EncodeToString(b)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func (h *Handlers) flushProductCache(ctx context.Context) {
	if h.cache != nil {
		_ = h.cache.FlushPattern(ctx, "tyrelink:product*")
		_ = h.cache.Delete(ctx, "tyrelink:admin:stats")
	}
}

func (h *Handlers) recordAudit(ctx context.Context, action, entityType, entityID string, details map[string]any) {
	if h.db == nil || !h.db.IsPostgres() {
		return
	}
	actor := middleware.ActorFromContext(ctx)
	detailsJSON, _ := json.Marshal(details)
	if _, err := h.db.Pool.Exec(ctx, `
		INSERT INTO audit_logs (id, actor, action, entity_type, entity_id, details, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
	`, randomHexID("aud", 8), actor, action, entityType, entityID, detailsJSON); err != nil {
		log.Printf("AUDIT WARN: failed recording %s (%s/%s): %v", action, entityType, entityID, err)
	}
}

func hydrateProduct(p *models.Product, specsRaw, imagesRaw []byte) {
	p.Title = p.Name
	p.Price = p.PriceMinor / 100
	p.OriginalPrice = p.OriginalPriceMinor / 100
	p.Currency = "LKR"
	if len(imagesRaw) > 0 {
		_ = json.Unmarshal(imagesRaw, &p.Images)
	}
	if len(p.Images) == 0 && p.FeaturedImage != "" {
		p.Images = []string{p.FeaturedImage}
	}
	if len(specsRaw) > 0 {
		_ = json.Unmarshal(specsRaw, &p.Specs)
	}
	if p.Specs == nil {
		p.Specs = map[string]any{}
	}
	if rawFaqs, ok := p.Specs["_faqs"]; ok {
		if b, err := json.Marshal(rawFaqs); err == nil {
			var parsed []models.ProductFAQ
			if json.Unmarshal(b, &parsed) == nil && len(parsed) > 0 {
				p.FAQs = parsed
			}
		}
		delete(p.Specs, "_faqs")
	}
}

func isSafeImageRef(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "/") && !strings.HasPrefix(lower, "//") {
		return true
	}
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return true
	}
	if (strings.HasPrefix(lower, "data:image/webp;base64,") ||
		strings.HasPrefix(lower, "data:image/jpeg;base64,") ||
		strings.HasPrefix(lower, "data:image/png;base64,")) && len(s) <= 220000 {
		return true
	}
	return false
}

func storageSpecsWithFAQs(p models.Product) map[string]any {
	out := make(map[string]any, len(p.Specs)+1)
	for k, v := range p.Specs {
		if k == "_faqs" {
			continue
		}
		out[k] = v
	}
	if len(p.FAQs) > 0 {
		out["_faqs"] = p.FAQs
	}
	return out
}

// Health Check
func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	redisOK := h.cache != nil && h.cache.IsRedis()
	pgOK := h.db != nil && h.db.IsPostgres()
	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"service":  "tyrelink-api",
		"time":     time.Now().UTC(),
		"postgres": pgOK,
		"redis":    redisOK,
	})
}

// GET /v1/products
func (h *Handlers) GetProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	cat := strings.TrimSpace(q.Get("category"))
	brand := strings.TrimSpace(q.Get("brand"))
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	flashOnly := q.Get("flashDeal") == "true"
	bestOnly := q.Get("bestSeller") == "true"
	inStockOnly := q.Get("inStock") == "true"
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	hasFilters := cat != "" || brand != "" || search != "" || flashOnly || bestOnly || inStockOnly || limit > 0 || offset > 0

	// Use indexed SQL pushdown when PostgreSQL is connected and filters/pagination are requested
	if hasFilters && h.db.IsPostgres() {
		sqlLimit := limit
		if sqlLimit <= 0 || sqlLimit > 200 {
			sqlLimit = 200
		}
		sqlOffset := offset
		if sqlOffset < 0 {
			sqlOffset = 0
		}
		rows, err := h.db.Pool.Query(ctx, `
			SELECT id, sku, coalesce(slug, id), brand, name, coalesce(sinhala_title, ''), category, coalesce(size, ''),
			       price_minor, coalesce(original_price_minor, 0), coalesce(featured_image, '/hero-tyre.png'),
			       coalesce(images, '[]'::jsonb), coalesce(specs, '{}'::jsonb), coalesce(short_description, ''),
			       stock_count, in_stock, is_featured, is_best_seller, is_flash_deal, coalesce(flash_discount, 0),
			       coalesce(warranty, ''), rating, review_count, max_order_quantity,
			       published, created_at, updated_at, coalesce(youtube_review_id,''),
			       COUNT(*) OVER()::int AS total_count
			FROM products
			WHERE published = true
			  AND ($1 = '' OR lower(category) = lower($1))
			  AND ($2 = '' OR lower(brand) = lower($2))
			  AND ($3 = false OR is_flash_deal = true)
			  AND ($4 = false OR is_best_seller = true)
			  AND ($5 = false OR in_stock = true)
			  AND (
			    $6 = ''
			    OR to_tsvector('english', coalesce(name,'') || ' ' || coalesce(brand,'') || ' ' || coalesce(size,'') || ' ' || coalesce(category,'') || ' ' || coalesce(short_description,'')) @@ websearch_to_tsquery('english', $6)
			    OR (name || ' ' || brand || ' ' || coalesce(size,'') || ' ' || category || ' ' || coalesce(short_description,'') || ' ' || sku) ILIKE '%' || $6 || '%'
			  )
			ORDER BY created_at DESC
			LIMIT $7 OFFSET $8
		`, cat, brand, flashOnly, bestOnly, inStockOnly, search, sqlLimit, sqlOffset)
		if err == nil {
			defer rows.Close()
			filtered := []models.Product{}
			total := 0
			for rows.Next() {
				var p models.Product
				var specsRaw, imagesRaw []byte
				var rowTotal int
				if rows.Scan(
					&p.ID, &p.SKU, &p.Slug, &p.Brand, &p.Name, &p.SinhalaTitle, &p.Category, &p.Size,
					&p.PriceMinor, &p.OriginalPriceMinor, &p.FeaturedImage, &imagesRaw, &specsRaw, &p.ShortDescription,
					&p.StockCount, &p.InStock, &p.IsFeatured, &p.IsBestSeller, &p.IsFlashDeal, &p.FlashDealDiscount,
					&p.Warranty, &p.Rating, &p.ReviewCount, &p.MaxOrderQuantity, &p.Published, &p.CreatedAt, &p.UpdatedAt, &p.YouTubeReviewID,
					&rowTotal,
				) == nil {
					total = rowTotal
					hydrateProduct(&p, specsRaw, imagesRaw)
					filtered = append(filtered, p)
				}
			}
			middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": filtered, "total": total, "cached": false})
			return
		}
	}

	cacheKey := "tyrelink:products:published"
	items := []models.Product{}

	usedCache := false
	if h.cache != nil && h.cache.Get(ctx, cacheKey, &items) {
		usedCache = true
	} else if !h.db.IsPostgres() {
		items = h.db.GetMemoryProducts()
		if h.cache != nil {
			_ = h.cache.Set(ctx, cacheKey, items, 5*time.Minute)
		}
	} else {
		rows, err := h.db.Pool.Query(ctx, `
			SELECT id, sku, coalesce(slug, id), brand, name, coalesce(sinhala_title, ''), category, coalesce(size, ''),
			       price_minor, coalesce(original_price_minor, 0), coalesce(featured_image, '/hero-tyre.png'),
			       coalesce(images, '[]'::jsonb), coalesce(specs, '{}'::jsonb), coalesce(short_description, ''),
			       stock_count, in_stock, is_featured, is_best_seller, is_flash_deal, coalesce(flash_discount, 0),
			       coalesce(warranty, ''), rating, review_count, max_order_quantity,
			       published, created_at, updated_at, coalesce(youtube_review_id,'')
			FROM products
			WHERE published=true
			ORDER BY created_at DESC
		`)
		if err != nil {
			items = h.db.GetMemoryProducts()
			middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": items, "total": len(items), "source": "memory_fallback"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var p models.Product
			var specsRaw, imagesRaw []byte
			err := rows.Scan(
				&p.ID, &p.SKU, &p.Slug, &p.Brand, &p.Name, &p.SinhalaTitle, &p.Category, &p.Size,
				&p.PriceMinor, &p.OriginalPriceMinor, &p.FeaturedImage, &imagesRaw, &specsRaw, &p.ShortDescription,
				&p.StockCount, &p.InStock, &p.IsFeatured, &p.IsBestSeller, &p.IsFlashDeal, &p.FlashDealDiscount,
				&p.Warranty, &p.Rating, &p.ReviewCount, &p.MaxOrderQuantity, &p.Published, &p.CreatedAt, &p.UpdatedAt, &p.YouTubeReviewID,
			)
			if err == nil {
				hydrateProduct(&p, specsRaw, imagesRaw)
				items = append(items, p)
			}
		}

		if h.cache != nil {
			_ = h.cache.Set(ctx, cacheKey, items, 5*time.Minute)
		}
	}

	if hasFilters {
		filtered := make([]models.Product, 0, len(items))
		for _, p := range items {
			if cat != "" && !strings.EqualFold(p.Category, cat) {
				continue
			}
			if brand != "" && !strings.EqualFold(p.Brand, brand) {
				continue
			}
			if flashOnly && !p.IsFlashDeal {
				continue
			}
			if bestOnly && !p.IsBestSeller {
				continue
			}
			if inStockOnly && !p.InStock {
				continue
			}
			if search != "" {
				haystack := strings.ToLower(p.Name + " " + p.Brand + " " + p.Category + " " + p.Size + " " + p.ShortDescription + " " + p.SKU)
				if !strings.Contains(haystack, search) {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		total := len(filtered)
		if offset > 0 {
			if offset >= len(filtered) {
				filtered = []models.Product{}
			} else {
				filtered = filtered[offset:]
			}
		}
		if limit > 0 && limit < len(filtered) {
			filtered = filtered[:limit]
		}
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": filtered, "total": total, "cached": usedCache})
		return
	}

	middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": items, "total": len(items), "cached": usedCache})
}

// GET /v1/products/{id}
func (h *Handlers) GetProductByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	cacheKey := "tyrelink:product:" + id

	var cached models.Product
	if h.cache != nil && h.cache.Get(ctx, cacheKey, &cached) {
		middleware.JSONWrite(w, http.StatusOK, cached)
		return
	}

	if !h.db.IsPostgres() {
		if p, ok := h.db.GetMemoryProductByID(id); ok {
			if h.cache != nil {
				_ = h.cache.Set(ctx, cacheKey, p, 5*time.Minute)
			}
			middleware.JSONWrite(w, http.StatusOK, p)
			return
		}
		middleware.JSONError(w, http.StatusNotFound, "product not found")
		return
	}

	var p models.Product
	var specsRaw, imagesRaw []byte
	err := h.db.Pool.QueryRow(ctx, `
		SELECT id, sku, coalesce(slug, id), brand, name, coalesce(sinhala_title, ''), category, coalesce(size, ''),
		       price_minor, coalesce(original_price_minor, 0), coalesce(featured_image, '/hero-tyre.png'),
		       coalesce(images, '[]'::jsonb), coalesce(specs, '{}'::jsonb), coalesce(short_description, ''),
		       stock_count, in_stock, is_featured, is_best_seller, is_flash_deal, coalesce(flash_discount, 0),
		       coalesce(warranty, 'Manufacturer Warranty'), rating, review_count, max_order_quantity,
		       published, created_at, updated_at, coalesce(youtube_review_id,'')
		FROM products
		WHERE (id=$1 OR slug=$1) AND published=true
	`, id).Scan(
		&p.ID, &p.SKU, &p.Slug, &p.Brand, &p.Name, &p.SinhalaTitle, &p.Category, &p.Size,
		&p.PriceMinor, &p.OriginalPriceMinor, &p.FeaturedImage, &imagesRaw, &specsRaw, &p.ShortDescription,
		&p.StockCount, &p.InStock, &p.IsFeatured, &p.IsBestSeller, &p.IsFlashDeal, &p.FlashDealDiscount,
		&p.Warranty, &p.Rating, &p.ReviewCount, &p.MaxOrderQuantity, &p.Published, &p.CreatedAt, &p.UpdatedAt, &p.YouTubeReviewID,
	)
	if err != nil {
		if mem, ok := h.db.GetMemoryProductByID(id); ok {
			middleware.JSONWrite(w, http.StatusOK, mem)
			return
		}
		middleware.JSONError(w, http.StatusNotFound, "product not found")
		return
	}

	hydrateProduct(&p, specsRaw, imagesRaw)
	if h.cache != nil {
		_ = h.cache.Set(ctx, cacheKey, p, 5*time.Minute)
	}
	middleware.JSONWrite(w, http.StatusOK, p)
}

// POST /v1/order-leads
func (h *Handlers) CreateOrderLead(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		middleware.JSONError(w, http.StatusBadRequest, "request body required")
		return
	}
	defer r.Body.Close()

	var payload struct {
		OrderRef      string                 `json:"orderRef"`
		CustomerName  string                 `json:"customerName"`
		Address       string                 `json:"address"`
		Phone1        string                 `json:"phone1"`
		Phone2        string                 `json:"phone2"`
		PaymentMethod string                 `json:"paymentMethod"`
		Items         []models.OrderLeadItem `json:"items"`
		Subtotal      int64                  `json:"subtotal"`
		CourierCharge int64                  `json:"courierCharge"`
		GrandTotal    int64                  `json:"grandTotal"`
	}

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128000)).Decode(&payload); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid order lead payload")
		return
	}

	payload.CustomerName = strings.TrimSpace(payload.CustomerName)
	payload.Phone1 = strings.TrimSpace(payload.Phone1)
	payload.Phone2 = strings.TrimSpace(payload.Phone2)
	payload.Address = strings.TrimSpace(payload.Address)
	payload.PaymentMethod = strings.TrimSpace(payload.PaymentMethod)

	if len(payload.CustomerName) < 2 || len(payload.Phone1) < 7 || len(payload.Items) == 0 || len(payload.Items) > 50 {
		middleware.JSONError(w, http.StatusBadRequest, "customerName, phone1, and 1–50 items are required")
		return
	}
	if payload.PaymentMethod == "" {
		payload.PaymentMethod = "Cash on Delivery"
	}

	// Authoritative server-side price & flat delivery calculation (never trust client-supplied prices)
	type authProductInfo struct {
		Title         string
		Price         int64
		MaxOrderQty   int
		InStock       bool
	}
	catalogLookup := make(map[string]authProductInfo, len(payload.Items)*2)
	idList := make([]string, 0, len(payload.Items))
	for _, it := range payload.Items {
		cleanID := strings.TrimSpace(it.ID)
		if cleanID == "" {
			middleware.JSONError(w, http.StatusBadRequest, "every order item requires a valid product id")
			return
		}
		idList = append(idList, cleanID)
	}

	var authoritativeCourier int64 = 450
	if h.db.IsPostgres() {
		pRows, err := h.db.Pool.Query(r.Context(), `
			SELECT id, coalesce(slug, id), name, price_minor, max_order_quantity, in_stock
			FROM products
			WHERE published = true AND (id = ANY($1::text[]) OR slug = ANY($1::text[]))
		`, idList)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "could not verify product pricing")
			return
		}
		for pRows.Next() {
			var pid, pslug, pname string
			var priceMinor int64
			var maxQty int
			var inStock bool
			if pRows.Scan(&pid, &pslug, &pname, &priceMinor, &maxQty, &inStock) == nil {
				info := authProductInfo{Title: pname, Price: priceMinor / 100, MaxOrderQty: maxQty, InStock: inStock}
				catalogLookup[pid] = info
				catalogLookup[pslug] = info
			}
		}
		pRows.Close()

		var fee int64
		if h.db.Pool.QueryRow(r.Context(), `SELECT fee FROM delivery_rates ORDER BY (town='Islandwide') DESC, town LIMIT 1`).Scan(&fee) == nil && fee >= 0 {
			authoritativeCourier = fee
		}
	} else {
		for _, id := range idList {
			if p, ok := h.db.GetMemoryProductByID(id); ok {
				info := authProductInfo{Title: p.Name, Price: p.Price, MaxOrderQty: p.MaxOrderQuantity, InStock: p.InStock}
				catalogLookup[p.ID] = info
				catalogLookup[p.Slug] = info
			}
		}
	}

	var calcSubtotal int64
	for i := range payload.Items {
		info, exists := catalogLookup[strings.TrimSpace(payload.Items[i].ID)]
		if !exists {
			middleware.JSONError(w, http.StatusBadRequest, "one or more selected products are unavailable")
			return
		}
		qty := payload.Items[i].Quantity
		if qty <= 0 {
			qty = 1
		}
		maxAllowed := info.MaxOrderQty
		if maxAllowed <= 0 {
			maxAllowed = 20
		}
		if qty > maxAllowed {
			qty = maxAllowed
		}
		payload.Items[i].Quantity = qty
		payload.Items[i].Price = info.Price
		if strings.TrimSpace(payload.Items[i].Title) == "" {
			payload.Items[i].Title = info.Title
		}
		calcSubtotal += info.Price * int64(qty)
	}
	payload.Subtotal = calcSubtotal
	if strings.Contains(strings.ToLower(payload.PaymentMethod), "pickup") {
		authoritativeCourier = 0
	}
	payload.CourierCharge = authoritativeCourier
	payload.GrandTotal = payload.Subtotal + payload.CourierCharge

	leadID := randomHexID("lead_"+time.Now().UTC().Format("20060102150405"), 4)
	if strings.TrimSpace(payload.OrderRef) == "" {
		payload.OrderRef = "TL-" + strings.ToUpper(randomHexID("", 3))
	}

	now := time.Now().UTC()
	lead := models.OrderLead{
		ID:            leadID,
		OrderRef:      payload.OrderRef,
		CustomerName:  payload.CustomerName,
		Address:       payload.Address,
		Phone1:        payload.Phone1,
		Phone2:        payload.Phone2,
		PaymentMethod: payload.PaymentMethod,
		Items:         payload.Items,
		Subtotal:      payload.Subtotal,
		CourierCharge: payload.CourierCharge,
		GrandTotal:    payload.GrandTotal,
		Status:        "new",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	h.db.AddMemoryLead(lead)

	if h.db.IsPostgres() {
		itemsJSON, _ := json.Marshal(lead.Items)
		payloadJSON, _ := json.Marshal(payload)
		_, err := h.db.Pool.Exec(r.Context(), `
			INSERT INTO order_leads (id, order_ref, customer_name, phone1, phone2, address, payment_method, subtotal_minor, courier_minor, grand_total_minor, items, payload, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'new', now(), now())
			ON CONFLICT (id) DO NOTHING
		`, lead.ID, lead.OrderRef, lead.CustomerName, lead.Phone1, lead.Phone2, lead.Address, lead.PaymentMethod, lead.Subtotal*100, lead.CourierCharge*100, lead.GrandTotal*100, itemsJSON, payloadJSON)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "order lead could not be stored")
			return
		}
	}

	if h.cache != nil {
		_ = h.cache.Delete(r.Context(), "tyrelink:admin:leads", "tyrelink:admin:stats")
	}

	middleware.JSONWrite(w, http.StatusCreated, map[string]any{
		"leadId":    lead.ID,
		"orderRef":  lead.OrderRef,
		"status":    "new",
		"createdAt": lead.CreatedAt,
	})
}

// POST /v1/admin/login
func (h *Handlers) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	defer r.Body.Close()

	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Password == "" {
		middleware.JSONError(w, http.StatusBadRequest, "password is required")
		return
	}

	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(h.cfg.AdminPassword)) != 1 {
		middleware.JSONError(w, http.StatusUnauthorized, "invalid admin credentials")
		return
	}

	token := h.mw.SignAdminSession("admin")
	h.mw.SetSessionCookie(w, r, token)
	h.recordAudit(context.Background(), "admin.login", "session", "admin", map[string]any{"remote": r.RemoteAddr})

	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"role":    "admin",
		"expires": time.Now().Add(12 * time.Hour).Unix(),
	})
}

// POST /v1/admin/logout
func (h *Handlers) AdminLogout(w http.ResponseWriter, r *http.Request) {
	if h.mw != nil {
		h.mw.ClearSessionCookie(w, r)
	}
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true})
}

// GET /v1/admin/verify
func (h *Handlers) AdminVerify(w http.ResponseWriter, r *http.Request) {
	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"role":          middleware.ActorFromContext(r.Context()),
	})
}

// GET /v1/admin/leads
func (h *Handlers) GetAdminLeads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	statusFilter := strings.TrimSpace(strings.ToLower(q.Get("status")))
	searchFilter := strings.TrimSpace(strings.ToLower(q.Get("q")))

	leads := []models.OrderLead{}

	if !h.db.IsPostgres() {
		leads = h.db.GetMemoryLeads()
	} else {
		cacheKey := "tyrelink:admin:leads"
		if statusFilter == "" && searchFilter == "" && h.cache != nil && h.cache.Get(ctx, cacheKey, &leads) {
			middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": leads, "cached": true})
			return
		}

		rows, err := h.db.Pool.Query(ctx, `
			SELECT id, coalesce(order_ref, ''), coalesce(customer_name, ''), coalesce(phone1, ''), coalesce(phone2, ''),
			       coalesce(address, ''), coalesce(payment_method, ''), coalesce(subtotal_minor, 0), coalesce(courier_minor, 0),
			       coalesce(grand_total_minor, 0), coalesce(items, '[]'::jsonb), status, coalesce(admin_notes, ''),
			       created_at, coalesce(updated_at, created_at)
			FROM order_leads
			ORDER BY created_at DESC
			LIMIT 250
		`)
		if err != nil {
			leads = h.db.GetMemoryLeads()
		} else {
			defer rows.Close()
			for rows.Next() {
				var l models.OrderLead
				var itemsRaw []byte
				var subMinor, courMinor, grandMinor int64
				err := rows.Scan(
					&l.ID, &l.OrderRef, &l.CustomerName, &l.Phone1, &l.Phone2,
					&l.Address, &l.PaymentMethod, &subMinor, &courMinor,
					&grandMinor, &itemsRaw, &l.Status, &l.AdminNotes, &l.CreatedAt, &l.UpdatedAt,
				)
				if err == nil {
					l.Subtotal = subMinor / 100
					l.CourierCharge = courMinor / 100
					l.GrandTotal = grandMinor / 100
					_ = json.Unmarshal(itemsRaw, &l.Items)
					if l.Items == nil {
						l.Items = []models.OrderLeadItem{}
					}
					leads = append(leads, l)
				}
			}
			if h.cache != nil {
				_ = h.cache.Set(ctx, cacheKey, leads, 30*time.Second)
			}
		}
	}

	if statusFilter != "" || searchFilter != "" {
		filtered := make([]models.OrderLead, 0, len(leads))
		for _, l := range leads {
			if statusFilter != "" && statusFilter != "all" && !strings.EqualFold(l.Status, statusFilter) {
				continue
			}
			if searchFilter != "" {
				haystack := strings.ToLower(l.OrderRef + " " + l.CustomerName + " " + l.Phone1 + " " + l.Address + " " + l.AdminNotes)
				if !strings.Contains(haystack, searchFilter) {
					continue
				}
			}
			filtered = append(filtered, l)
		}
		leads = filtered
	}

	middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": leads, "cached": false})
}

var validLeadStatuses = map[string]bool{
	"new": true, "contacted": true, "qualified": true, "dispatched": true,
	"delivered": true, "cancelled": true, "closed": true, "spam": true,
}

// PUT /v1/admin/leads/{id}
func (h *Handlers) UpdateAdminLead(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	defer r.Body.Close()

	var body struct {
		Status     string `json:"status"`
		AdminNotes string `json:"adminNotes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid lead update payload")
		return
	}

	body.Status = strings.TrimSpace(strings.ToLower(body.Status))
	body.AdminNotes = strings.TrimSpace(body.AdminNotes)
	if !validLeadStatuses[body.Status] {
		middleware.JSONError(w, http.StatusBadRequest, "invalid lead status")
		return
	}

	if h.db.IsPostgres() {
		tag, err := h.db.Pool.Exec(r.Context(), `
			UPDATE order_leads SET status=$1, admin_notes=$2, updated_at=now() WHERE id=$3
		`, body.Status, body.AdminNotes, id)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "failed to update lead")
			return
		}
		if tag.RowsAffected() == 0 {
			middleware.JSONError(w, http.StatusNotFound, "lead not found")
			return
		}
	} else if !h.db.UpdateMemoryLead(id, body.Status, body.AdminNotes) {
		middleware.JSONError(w, http.StatusNotFound, "lead not found")
		return
	}

	if h.cache != nil {
		_ = h.cache.Delete(r.Context(), "tyrelink:admin:leads", "tyrelink:admin:stats")
	}
	h.recordAudit(r.Context(), "lead.update", "order_lead", id, map[string]any{"status": body.Status})
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "id": id, "status": body.Status})
}

// DELETE /v1/admin/leads/{id}
func (h *Handlers) DeleteAdminLead(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.db.IsPostgres() {
		tag, err := h.db.Pool.Exec(r.Context(), `DELETE FROM order_leads WHERE id=$1`, id)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "failed to delete lead")
			return
		}
		if tag.RowsAffected() == 0 {
			middleware.JSONError(w, http.StatusNotFound, "lead not found")
			return
		}
	} else if !h.db.DeleteMemoryLead(id) {
		middleware.JSONError(w, http.StatusNotFound, "lead not found")
		return
	}

	if h.cache != nil {
		_ = h.cache.Delete(r.Context(), "tyrelink:admin:leads", "tyrelink:admin:stats")
	}
	h.recordAudit(r.Context(), "lead.delete", "order_lead", id, nil)
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "deleted": true, "id": id})
}

func normalizeIncomingProduct(p *models.Product) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Title = strings.TrimSpace(p.Title)
	if p.Name == "" {
		p.Name = p.Title
	}
	if p.Title == "" {
		p.Title = p.Name
	}
	p.Brand = strings.TrimSpace(p.Brand)
	p.Category = strings.TrimSpace(p.Category)
	if p.Category == "" {
		p.Category = "car-tyres"
	}

	if p.ID == "" || p.Name == "" || p.Brand == "" {
		return fmt.Errorf("id, name/title, and brand are required")
	}
	if p.Price < 0 || p.PriceMinor < 0 || p.StockCount < 0 {
		return fmt.Errorf("price and stock cannot be negative")
	}

	if p.Slug == "" {
		base := strings.ToLower(p.Name)
		base = strings.ReplaceAll(base, " ", "-")
		p.Slug = strings.Trim(slugCleanRe.ReplaceAllString(base, "-"), "-")
		if p.Slug == "" {
			p.Slug = p.ID
		}
	}
	if p.SKU == "" {
		p.SKU = "TL-" + strings.ToUpper(strings.ReplaceAll(p.ID, "-", ""))
		if len(p.SKU) > 28 {
			p.SKU = p.SKU[:28]
		}
	}

	if p.PriceMinor == 0 && p.Price > 0 {
		p.PriceMinor = p.Price * 100
	} else if p.Price == 0 && p.PriceMinor > 0 {
		p.Price = p.PriceMinor / 100
	}
	if p.OriginalPriceMinor == 0 && p.OriginalPrice > 0 {
		p.OriginalPriceMinor = p.OriginalPrice * 100
	} else if p.OriginalPrice == 0 && p.OriginalPriceMinor > 0 {
		p.OriginalPrice = p.OriginalPriceMinor / 100
	}
	if p.FeaturedImage == "" {
		p.FeaturedImage = "/hero-tyre.png"
	} else if !isSafeImageRef(p.FeaturedImage) {
		return fmt.Errorf("invalid featuredImage scheme; use https://, /, or compressed raster base64")
	}
	if len(p.Images) == 0 {
		p.Images = []string{p.FeaturedImage}
	} else {
		cleanImgs := make([]string, 0, len(p.Images))
		for _, img := range p.Images {
			if isSafeImageRef(img) {
				cleanImgs = append(cleanImgs, strings.TrimSpace(img))
			}
		}
		if len(cleanImgs) == 0 {
			cleanImgs = []string{p.FeaturedImage}
		}
		p.Images = cleanImgs
	}
	if p.MaxOrderQuantity <= 0 {
		p.MaxOrderQuantity = 4
	}
	if p.Specs == nil {
		p.Specs = map[string]any{}
	}
	delete(p.Specs, "_faqs")
	if len(p.FAQs) > 0 {
		cleaned := make([]models.ProductFAQ, 0, len(p.FAQs))
		for _, f := range p.FAQs {
			q := strings.TrimSpace(f.Question)
			a := strings.TrimSpace(f.Answer)
			if q != "" && a != "" {
				cleaned = append(cleaned, models.ProductFAQ{Question: q, Answer: a})
			}
		}
		p.FAQs = cleaned
	}
	if p.Size != "" {
		if _, ok := p.Specs["Size"]; !ok {
			p.Specs["Size"] = p.Size
		}
	} else if s, ok := p.Specs["Size"].(string); ok {
		p.Size = s
	}
	if p.Warranty != "" {
		p.Warranty = ""
	}
	p.Currency = "LKR"
	return nil
}

// POST /v1/admin/products
func (h *Handlers) AdminCreateProduct(w http.ResponseWriter, r *http.Request) {
	var p models.Product
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512000)).Decode(&p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid product body")
		return
	}
	if !p.Published {
		p.Published = true
	}
	if err := normalizeIncomingProduct(&p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.db.SaveMemoryProduct(p)

	if h.db.IsPostgres() {
		specsJSON, _ := json.Marshal(storageSpecsWithFAQs(p))
		imagesJSON, _ := json.Marshal(p.Images)
		_, err := h.db.Pool.Exec(r.Context(), `
			INSERT INTO products (
				id, sku, slug, brand, name, sinhala_title, category, size,
				price_minor, original_price_minor, featured_image, images, specs,
				short_description, stock_count, in_stock, is_featured, is_best_seller,
				is_flash_deal, flash_discount, warranty, max_order_quantity, published,
				created_at, updated_at, youtube_review_id
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13,
				$14, $15, $16, $17, $18,
				$19, $20, $21, $22, $23,
				now(), now(), $24
			)
			ON CONFLICT (id) DO UPDATE SET
				slug = EXCLUDED.slug, name = EXCLUDED.name, sinhala_title = EXCLUDED.sinhala_title,
				brand = EXCLUDED.brand, category = EXCLUDED.category, size = EXCLUDED.size,
				price_minor = EXCLUDED.price_minor, original_price_minor = EXCLUDED.original_price_minor,
				featured_image = EXCLUDED.featured_image, images = EXCLUDED.images, specs = EXCLUDED.specs,
				short_description = EXCLUDED.short_description, stock_count = EXCLUDED.stock_count,
				in_stock = EXCLUDED.in_stock, is_featured = EXCLUDED.is_featured,
				is_best_seller = EXCLUDED.is_best_seller, is_flash_deal = EXCLUDED.is_flash_deal,
				flash_discount = EXCLUDED.flash_discount, warranty = EXCLUDED.warranty,
				max_order_quantity = EXCLUDED.max_order_quantity, published = EXCLUDED.published,
				youtube_review_id = EXCLUDED.youtube_review_id, updated_at = now()
		`, p.ID, p.SKU, p.Slug, p.Brand, p.Name, p.SinhalaTitle, p.Category, p.Size,
			p.PriceMinor, p.OriginalPriceMinor, p.FeaturedImage, imagesJSON, specsJSON,
			p.ShortDescription, p.StockCount, p.InStock, p.IsFeatured, p.IsBestSeller,
			p.IsFlashDeal, p.FlashDealDiscount, p.Warranty, p.MaxOrderQuantity, p.Published, p.YouTubeReviewID)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "Product could not be saved to the database")
			return
		}
	}

	h.flushProductCache(r.Context())
	h.recordAudit(r.Context(), "product.create", "product", p.ID, map[string]any{"name": p.Name, "price": p.Price})
	middleware.JSONWrite(w, http.StatusCreated, map[string]any{"success": true, "product": p})
}

// PUT /v1/admin/products/{id}
func (h *Handlers) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var rawMap map[string]any
	defer r.Body.Close()

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512000))
	rawBytes, err := json.RawMessage{}, error(nil)
	if err = dec.Decode(&rawBytes); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid product body")
		return
	}
	_ = json.Unmarshal(rawBytes, &rawMap)

	var p models.Product
	if err := json.Unmarshal(rawBytes, &p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid product fields")
		return
	}
	p.ID = id
	// Preserve published=true unless explicitly set to false in JSON payload
	if _, hasPub := rawMap["published"]; !hasPub {
		p.Published = true
	}

	if err := normalizeIncomingProduct(&p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.db.SaveMemoryProduct(p)

	if h.db.IsPostgres() {
		specsJSON, _ := json.Marshal(storageSpecsWithFAQs(p))
		imagesJSON, _ := json.Marshal(p.Images)
		tag, err := h.db.Pool.Exec(r.Context(), `
			UPDATE products
			SET name=$1, sinhala_title=$2, brand=$3, category=$4, size=$5,
			    price_minor=$6, original_price_minor=$7, short_description=$8,
			    stock_count=$9, in_stock=$10, is_featured=$11, is_best_seller=$12,
			    is_flash_deal=$13, flash_discount=$14, specs=$15, youtube_review_id=$16,
			    featured_image=$17, images=$18, warranty=$19, max_order_quantity=$20,
			    published=$21, updated_at=now()
			WHERE id=$22
		`, p.Name, p.SinhalaTitle, p.Brand, p.Category, p.Size,
			p.PriceMinor, p.OriginalPriceMinor, p.ShortDescription,
			p.StockCount, p.InStock, p.IsFeatured, p.IsBestSeller,
			p.IsFlashDeal, p.FlashDealDiscount, specsJSON, p.YouTubeReviewID,
			p.FeaturedImage, imagesJSON, p.Warranty, p.MaxOrderQuantity,
			p.Published, id)
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "Product could not be updated in database")
			return
		}
		if tag.RowsAffected() == 0 {
			middleware.JSONError(w, http.StatusNotFound, "product not found")
			return
		}
	}

	h.flushProductCache(r.Context())
	h.recordAudit(r.Context(), "product.update", "product", id, map[string]any{"name": p.Name, "price": p.Price, "inStock": p.InStock})
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "id": id, "product": p})
}

// DELETE /v1/admin/products/{id}
func (h *Handlers) AdminDeleteProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	permanent := r.URL.Query().Get("permanent") == "true"

	h.db.DeleteMemoryProduct(id, permanent)

	if h.db.IsPostgres() {
		var err error
		if permanent {
			_, err = h.db.Pool.Exec(r.Context(), `DELETE FROM products WHERE id=$1`, id)
		} else {
			_, err = h.db.Pool.Exec(r.Context(), `UPDATE products SET published=false, updated_at=now() WHERE id=$1`, id)
		}
		if err != nil {
			middleware.JSONError(w, http.StatusServiceUnavailable, "could not delete product")
			return
		}
	}

	h.flushProductCache(r.Context())
	h.recordAudit(r.Context(), "product.delete", "product", id, map[string]any{"permanent": permanent})
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "deleted": true, "permanent": permanent, "id": id})
}
