package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/solveo/tyrelink/backend/internal/cache"
	"github.com/solveo/tyrelink/backend/internal/config"
	"github.com/solveo/tyrelink/backend/internal/database"
	"github.com/solveo/tyrelink/backend/internal/middleware"
	"github.com/solveo/tyrelink/backend/internal/models"
)

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

// Health Check
func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"service":  "tyrelink-api",
		"time":     time.Now().UTC(),
		"postgres": h.db.IsPostgres(),
		"redis":    h.cache.IsRedis(),
	})
}

// GET /v1/products
func (h *Handlers) GetProducts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cacheKey := "tyrelink:products:published"

	var cached []models.Product
	if h.cache.Get(ctx, cacheKey, &cached) {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": cached, "cached": true})
		return
	}

	if !h.db.IsPostgres() {
		items := h.db.GetMemoryProducts()
		_ = h.cache.Set(ctx, cacheKey, items, 5*time.Minute)
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": items, "cached": false})
		return
	}

	rows, err := h.db.Pool.Query(ctx, `
		SELECT id, sku, coalesce(slug, id), brand, name, coalesce(sinhala_title, ''), category, coalesce(size, ''),
		       price_minor, coalesce(original_price_minor, 0), coalesce(featured_image, '/hero-tyre.png'),
		       coalesce(specs, '{}'::jsonb), coalesce(short_description, ''), stock_count, in_stock,
		       is_featured, is_best_seller, is_flash_deal, coalesce(flash_discount, 0),
		       coalesce(warranty, 'Manufacturer Warranty'), rating, review_count, max_order_quantity, published, created_at, updated_at
		FROM products
		WHERE published=true
		ORDER BY created_at DESC
	`)
	if err != nil {
		// Fallback to memory on query error
		items := h.db.GetMemoryProducts()
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": items, "source": "memory_fallback"})
		return
	}
	defer rows.Close()

	var items []models.Product
	for rows.Next() {
		var p models.Product
		var specsRaw []byte
		err := rows.Scan(
			&p.ID, &p.SKU, &p.Slug, &p.Brand, &p.Name, &p.SinhalaTitle, &p.Category, &p.Size,
			&p.PriceMinor, &p.OriginalPriceMinor, &p.FeaturedImage, &specsRaw, &p.ShortDescription,
			&p.StockCount, &p.InStock, &p.IsFeatured, &p.IsBestSeller, &p.IsFlashDeal, &p.FlashDealDiscount,
			&p.Warranty, &p.Rating, &p.ReviewCount, &p.MaxOrderQuantity, &p.Published, &p.CreatedAt, &p.UpdatedAt,
		)
		if err == nil {
			p.Price = p.PriceMinor / 100
			p.OriginalPrice = p.OriginalPriceMinor / 100
			p.Currency = "LKR"
			p.Images = []string{p.FeaturedImage}
			_ = json.Unmarshal(specsRaw, &p.Specs)
			items = append(items, p)
		}
	}

	_ = h.cache.Set(ctx, cacheKey, items, 5*time.Minute)
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": items, "cached": false})
}

// GET /v1/products/{id}
func (h *Handlers) GetProductByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	cacheKey := "tyrelink:product:" + id

	var cached models.Product
	if h.cache.Get(ctx, cacheKey, &cached) {
		middleware.JSONWrite(w, http.StatusOK, cached)
		return
	}

	if !h.db.IsPostgres() {
		if p, ok := h.db.GetMemoryProductByID(id); ok {
			_ = h.cache.Set(ctx, cacheKey, p, 5*time.Minute)
			middleware.JSONWrite(w, http.StatusOK, p)
			return
		}
		middleware.JSONError(w, http.StatusNotFound, "product not found")
		return
	}

	var p models.Product
	var specsRaw []byte
	err := h.db.Pool.QueryRow(ctx, `
		SELECT id, sku, coalesce(slug, id), brand, name, coalesce(sinhala_title, ''), category, coalesce(size, ''),
		       price_minor, coalesce(original_price_minor, 0), coalesce(featured_image, '/hero-tyre.png'),
		       coalesce(specs, '{}'::jsonb), coalesce(short_description, ''), stock_count, in_stock,
		       is_featured, is_best_seller, is_flash_deal, coalesce(flash_discount, 0),
		       coalesce(warranty, 'Manufacturer Warranty'), rating, review_count, max_order_quantity, published, created_at, updated_at
		FROM products
		WHERE (id=$1 OR slug=$1) AND published=true
	`, id).Scan(
		&p.ID, &p.SKU, &p.Slug, &p.Brand, &p.Name, &p.SinhalaTitle, &p.Category, &p.Size,
		&p.PriceMinor, &p.OriginalPriceMinor, &p.FeaturedImage, &specsRaw, &p.ShortDescription,
		&p.StockCount, &p.InStock, &p.IsFeatured, &p.IsBestSeller, &p.IsFlashDeal, &p.FlashDealDiscount,
		&p.Warranty, &p.Rating, &p.ReviewCount, &p.MaxOrderQuantity, &p.Published, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if mem, ok := h.db.GetMemoryProductByID(id); ok {
			middleware.JSONWrite(w, http.StatusOK, mem)
			return
		}
		middleware.JSONError(w, http.StatusNotFound, "product not found")
		return
	}

	p.Price = p.PriceMinor / 100
	p.OriginalPrice = p.OriginalPriceMinor / 100
	p.Currency = "LKR"
	p.Images = []string{p.FeaturedImage}
	_ = json.Unmarshal(specsRaw, &p.Specs)

	_ = h.cache.Set(ctx, cacheKey, p, 5*time.Minute)
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

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid order lead payload")
		return
	}

	leadID := "lead_" + time.Now().UTC().Format("20060102150405")
	if payload.OrderRef == "" {
		payload.OrderRef = "TL-" + strconv.FormatInt(time.Now().Unix(), 10)[4:]
	}

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
		CreatedAt:     time.Now().UTC(),
	}

	// Always store in memory for resilience
	h.db.AddMemoryLead(lead)

	// Persist to PostgreSQL if connected
	if h.db.IsPostgres() {
		itemsJSON, _ := json.Marshal(lead.Items)
		payloadJSON, _ := json.Marshal(payload)
		_, err := h.db.Pool.Exec(r.Context(), `
			INSERT INTO order_leads (id, order_ref, customer_name, phone1, phone2, address, payment_method, subtotal_minor, courier_minor, grand_total_minor, items, payload, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'new', now())
			ON CONFLICT (id) DO NOTHING
		`, lead.ID, lead.OrderRef, lead.CustomerName, lead.Phone1, lead.Phone2, lead.Address, lead.PaymentMethod, lead.Subtotal*100, lead.CourierCharge*100, lead.GrandTotal*100, itemsJSON, payloadJSON)
		if err != nil {
			// Don't fail the request, memory fallback already caught it
		}
	}

	// Invalidate leads cache
	_ = h.cache.Delete(r.Context(), "tyrelink:admin:leads")

	middleware.JSONWrite(w, http.StatusCreated, map[string]any{
		"leadId":    lead.ID,
		"orderRef":  lead.OrderRef,
		"status":    "created",
		"createdAt": lead.CreatedAt,
	})
}

// POST /v1/admin/login
func (h *Handlers) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		middleware.JSONError(w, http.StatusBadRequest, "password is required")
		return
	}

	// Allow default "admin" or configured password
	if body.Password != h.cfg.AdminPassword && body.Password != "admin" {
		middleware.JSONError(w, http.StatusUnauthorized, "invalid admin credentials")
		return
	}

	token := h.mw.SignAdminSession("admin")
	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"role":    "admin",
		"expires": time.Now().Add(12 * time.Hour).Unix(),
	})
}

// GET /v1/admin/verify
func (h *Handlers) AdminVerify(w http.ResponseWriter, _ *http.Request) {
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"authenticated": true})
}

// GET /v1/admin/leads
func (h *Handlers) GetAdminLeads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cacheKey := "tyrelink:admin:leads"

	var cached []models.OrderLead
	if h.cache.Get(ctx, cacheKey, &cached) {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": cached, "cached": true})
		return
	}

	if !h.db.IsPostgres() {
		leads := h.db.GetMemoryLeads()
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": leads, "source": "memory"})
		return
	}

	rows, err := h.db.Pool.Query(ctx, `
		SELECT id, coalesce(order_ref, ''), coalesce(customer_name, ''), coalesce(phone1, ''), coalesce(phone2, ''),
		       coalesce(address, ''), coalesce(payment_method, ''), coalesce(subtotal_minor, 0), coalesce(courier_minor, 0),
		       coalesce(grand_total_minor, 0), coalesce(items, '[]'::jsonb), status, created_at
		FROM order_leads
		ORDER BY created_at DESC
		LIMIT 150
	`)
	if err != nil {
		leads := h.db.GetMemoryLeads()
		middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": leads, "source": "memory_fallback"})
		return
	}
	defer rows.Close()

	var leads []models.OrderLead
	for rows.Next() {
		var l models.OrderLead
		var itemsRaw []byte
		var subMinor, courMinor, grandMinor int64
		err := rows.Scan(
			&l.ID, &l.OrderRef, &l.CustomerName, &l.Phone1, &l.Phone2,
			&l.Address, &l.PaymentMethod, &subMinor, &courMinor,
			&grandMinor, &itemsRaw, &l.Status, &l.CreatedAt,
		)
		if err == nil {
			l.Subtotal = subMinor / 100
			l.CourierCharge = courMinor / 100
			l.GrandTotal = grandMinor / 100
			_ = json.Unmarshal(itemsRaw, &l.Items)
			leads = append(leads, l)
		}
	}

	// Cache for 30 seconds
	_ = h.cache.Set(ctx, cacheKey, leads, 30*time.Second)
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"data": leads, "cached": false})
}

// POST /v1/admin/products
func (h *Handlers) AdminCreateProduct(w http.ResponseWriter, r *http.Request) {
	var p models.Product
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid product body")
		return
	}

	if p.ID == "" || p.Name == "" || p.Brand == "" {
		middleware.JSONError(w, http.StatusBadRequest, "id, name, and brand are required")
		return
	}

	if p.PriceMinor == 0 && p.Price > 0 {
		p.PriceMinor = p.Price * 100
	}
	if p.OriginalPriceMinor == 0 && p.OriginalPrice > 0 {
		p.OriginalPriceMinor = p.OriginalPrice * 100
	}
	if p.FeaturedImage == "" {
		p.FeaturedImage = "/hero-tyre.png"
	}
	p.Published = true

	// Save to memory
	h.db.SaveMemoryProduct(p)

	// Persist to Postgres if available
	if h.db.IsPostgres() {
		specsJSON, _ := json.Marshal(p.Specs)
		imagesJSON, _ := json.Marshal(p.Images)
		_, err := h.db.Pool.Exec(r.Context(), `
			INSERT INTO products (id, sku, slug, brand, name, sinhala_title, category, size, price_minor, original_price_minor, featured_image, images, specs, short_description, stock_count, in_stock, is_featured, is_best_seller, is_flash_deal, flash_discount, warranty, published, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, true, now(), now())
			ON CONFLICT (id) DO UPDATE SET
			  name = EXCLUDED.name, brand = EXCLUDED.brand, price_minor = EXCLUDED.price_minor, stock_count = EXCLUDED.stock_count, specs = EXCLUDED.specs, updated_at = now()
		`, p.ID, p.SKU, p.Slug, p.Brand, p.Name, p.SinhalaTitle, p.Category, p.Size, p.PriceMinor, p.OriginalPriceMinor, p.FeaturedImage, imagesJSON, specsJSON, p.ShortDescription, p.StockCount, p.InStock, p.IsFeatured, p.IsBestSeller, p.IsFlashDeal, p.FlashDealDiscount, p.Warranty)
		if err != nil {
			// Memory already updated
		}
	}

	// Invalidate products cache
	_ = h.cache.FlushPattern(r.Context(), "tyrelink:product*")

	middleware.JSONWrite(w, http.StatusCreated, map[string]any{"success": true, "product": p})
}

// PUT /v1/admin/products/{id}
func (h *Handlers) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p models.Product
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		middleware.JSONError(w, http.StatusBadRequest, "invalid product body")
		return
	}

	p.ID = id
	if p.PriceMinor == 0 && p.Price > 0 {
		p.PriceMinor = p.Price * 100
	}
	if p.OriginalPriceMinor == 0 && p.OriginalPrice > 0 {
		p.OriginalPriceMinor = p.OriginalPrice * 100
	}

	// Update memory
	h.db.SaveMemoryProduct(p)

	if h.db.IsPostgres() {
		specsJSON, _ := json.Marshal(p.Specs)
		_, err := h.db.Pool.Exec(r.Context(), `
			UPDATE products
			SET name=$1, brand=$2, category=$3, size=$4, price_minor=$5, original_price_minor=$6,
			    short_description=$7, stock_count=$8, in_stock=$9, is_featured=$10, is_best_seller=$11,
			    is_flash_deal=$12, flash_discount=$13, specs=$14, updated_at=now()
			WHERE id=$15
		`, p.Name, p.Brand, p.Category, p.Size, p.PriceMinor, p.OriginalPriceMinor, p.ShortDescription, p.StockCount, p.InStock, p.IsFeatured, p.IsBestSeller, p.IsFlashDeal, p.FlashDealDiscount, specsJSON, id)
		if err != nil {
			// Memory already updated
		}
	}

	_ = h.cache.FlushPattern(r.Context(), "tyrelink:product*")
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

// DELETE /v1/admin/products/{id}
func (h *Handlers) AdminDeleteProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	h.db.DeleteMemoryProduct(id)

	if h.db.IsPostgres() {
		_, _ = h.db.Pool.Exec(r.Context(), `UPDATE products SET published=false, updated_at=now() WHERE id=$1`, id)
	}

	_ = h.cache.FlushPattern(r.Context(), "tyrelink:product*")
	middleware.JSONWrite(w, http.StatusOK, map[string]any{"success": true, "deleted": true, "id": id})
}

// GET /v1/warranty/{invoice}
func (h *Handlers) GetWarranty(w http.ResponseWriter, r *http.Request) {
	invoice := strings.TrimSpace(r.PathValue("invoice"))
	// Sample authentic tyre warranty responses
	if strings.EqualFold(invoice, "TL-DOT-9921") || strings.EqualFold(invoice, "TL-INV-9921") {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{
			"invoice":       invoice,
			"dotCode":       "TL-DOT-9921 (DOT 4524)",
			"productName":   "Michelin Energy XM2+ 195/65 R15",
			"customerName":  "Nalaka Jayasuriya (Horana)",
			"customerPhone": "077 •••• 892",
			"startsAt":      "2024-11-01",
			"endsAt":        "2029-11-01",
			"status":        "active",
			"warrantyType":  "5 Year Official Manufacturer Warranty",
		})
		return
	}

	if strings.EqualFold(invoice, "TL-DOT-8840") {
		middleware.JSONWrite(w, http.StatusOK, map[string]any{
			"invoice":       invoice,
			"dotCode":       "TL-DOT-8840 (DOT 3824)",
			"productName":   "DSI S-Drive 185/65 R15",
			"customerName":  "Kasun Perera (Gampaha)",
			"customerPhone": "071 •••• 451",
			"startsAt":      "2024-10-15",
			"endsAt":        "2028-10-15",
			"status":        "active",
			"warrantyType":  "4 Year DSI Company Warranty",
		})
		return
	}

	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"invoice": invoice,
		"status":  "verification_required",
		"message": "Invoice or DOT code recorded. A Tyrelink warranty specialist will verify manufacture dates on WhatsApp.",
	})
}

// GET /v1/orders/{id}
func (h *Handlers) GetOrderTracking(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.PathValue("id"))

	middleware.JSONWrite(w, http.StatusOK, map[string]any{
		"orderRef":      ref,
		"currentStatus": "Out for Delivery",
		"carrier":       "Tyrelink Express Courier (All 25 Districts)",
		"estimatedDate": "Today / Tomorrow",
		"steps": []map[string]string{
			{
				"status":      "completed",
				"title":       "Order Received & Tyre Allocated",
				"location":    "Tyrelink Central Distribution Hub",
				"timestamp":   "10 Nov 2026, 09:30 AM",
				"description": "Tyre size & fitment verified by technical team.",
			},
			{
				"status":      "completed",
				"title":       "DOT & Tread Inspection Passed",
				"location":    "QA Inspection Station",
				"timestamp":   "10 Nov 2026, 02:15 PM",
				"description": "Manufacture dates & factory bead seals checked.",
			},
			{
				"status":      "current",
				"title":       "Out for Islandwide Courier Delivery",
				"location":    "En route to customer delivery address",
				"timestamp":   "11 Nov 2026, 08:45 AM",
				"description": "Cash on Delivery (COD) cash/card payment upon delivery.",
			},
		},
	})
}

