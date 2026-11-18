package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/solveo/tyrelink/backend/internal/middleware"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"
)

type review struct {
	PhotoDataURL string    `json:"photoDataUrl,omitempty"`
	ID           string    `json:"id"`
	ProductID    string    `json:"productId"`
	Name         string    `json:"name"`
	Rating       int       `json:"rating"`
	Text         string    `json:"text"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (h *Handlers) customerDB(w http.ResponseWriter) bool {
	if !h.db.IsPostgres() {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Persistent storage is unavailable. Please try again later."})
		return false
	}
	return true
}
func (h *Handlers) GetReviews(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	query := `SELECT id,product_id,name,rating,body,status,created_at,photo_data_url FROM product_reviews WHERE product_id=$1 AND status='approved' ORDER BY created_at DESC LIMIT 100`
	args := []any{r.PathValue("id")}
	if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
		query = `SELECT id,product_id,name,rating,body,status,created_at,photo_data_url FROM product_reviews ORDER BY created_at DESC LIMIT 200`
		args = nil
	}
	rows, err := h.db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Reviews are temporarily unavailable"})
		return
	}
	defer rows.Close()
	result := []review{}
	for rows.Next() {
		var v review
		if err := rows.Scan(&v.ID, &v.ProductID, &v.Name, &v.Rating, &v.Text, &v.Status, &v.CreatedAt, &v.PhotoDataURL); err != nil {
			middleware.JSONWrite(w, 500, map[string]string{"error": "Could not read reviews"})
			return
		}
		result = append(result, v)
	}
	if rows.Err() != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Could not read reviews"})
		return
	}
	middleware.JSONWrite(w, 200, map[string]any{"data": result})
}
func (h *Handlers) CreateReview(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	var v review
	r.Body = http.MaxBytesReader(w, r.Body, 420000)
	if json.NewDecoder(r.Body).Decode(&v) != nil {
		middleware.JSONWrite(w, 400, map[string]string{"error": "Invalid review"})
		return
	}
	if v.PhotoDataURL != "" {
		parts := strings.SplitN(v.PhotoDataURL, ",", 2)
		if len(parts) != 2 || (parts[0] != "data:image/jpeg;base64" && parts[0] != "data:image/png;base64") {
			middleware.JSONWrite(w, 400, map[string]string{"error": "Photo must be JPEG or PNG"})
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil || len(decoded) > 300*1024 {
			middleware.JSONWrite(w, 400, map[string]string{"error": "Photo must be smaller than 300 KB"})
			return
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(decoded))
		if err != nil || cfg.Width > 2000 || cfg.Height > 2000 {
			middleware.JSONWrite(w, 400, map[string]string{"error": "Invalid photo dimensions"})
			return
		}
	}
	v.Name = strings.TrimSpace(v.Name)
	v.Text = strings.TrimSpace(v.Text)
	if len([]rune(v.Name)) < 2 || len([]rune(v.Name)) > 80 || len([]rune(v.Text)) < 10 || len([]rune(v.Text)) > 2000 || v.Rating < 1 || v.Rating > 5 {
		middleware.JSONWrite(w, 400, map[string]string{"error": "Enter a name, a 1–5 rating and a review between 10 and 2000 characters"})
		return
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Try again later"})
		return
	}
	v.ID = hex.EncodeToString(b)
	tag, err := h.db.Pool.Exec(r.Context(), `INSERT INTO product_reviews(id,product_id,name,rating,body,photo_data_url) SELECT $1,id,$3,$4,$5,$6 FROM products WHERE id=$2 AND published=true`, v.ID, r.PathValue("id"), v.Name, v.Rating, v.Text, v.PhotoDataURL)
	if err != nil {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Review could not be saved"})
		return
	}
	if tag.RowsAffected() == 0 {
		middleware.JSONWrite(w, 404, map[string]string{"error": "Product not found"})
		return
	}
	middleware.JSONWrite(w, 201, map[string]string{"status": "pending", "id": v.ID})
}
func (h *Handlers) ModerateReview(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	var v struct {
		Status string `json:"status"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1000)).Decode(&v) != nil || (v.Status != "approved" && v.Status != "rejected" && v.Status != "pending") {
		middleware.JSONWrite(w, 400, map[string]string{"error": "Invalid status"})
		return
	}
	tag, err := h.db.Pool.Exec(r.Context(), `UPDATE product_reviews SET status=$1 WHERE id=$2`, v.Status, r.PathValue("id"))
	if err != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Could not update review"})
		return
	}
	if tag.RowsAffected() == 0 {
		middleware.JSONWrite(w, 404, map[string]string{"error": "Review not found"})
		return
	}
	middleware.JSONWrite(w, 200, map[string]bool{"success": true})
}

type deliveryRate struct {
	Town     string `json:"town"`
	Fee      int    `json:"fee"`
	Estimate string `json:"estimate"`
}

func (h *Handlers) GetDeliveryRates(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	rows, err := h.db.Pool.Query(r.Context(), `SELECT town,fee,estimate FROM delivery_rates ORDER BY town`)
	if err != nil {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Delivery estimates unavailable"})
		return
	}
	defer rows.Close()
	result := []deliveryRate{}
	for rows.Next() {
		var v deliveryRate
		if rows.Scan(&v.Town, &v.Fee, &v.Estimate) != nil {
			middleware.JSONWrite(w, 500, map[string]string{"error": "Could not read delivery rates"})
			return
		}
		result = append(result, v)
	}
	if rows.Err() != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Could not read delivery rates"})
		return
	}
	middleware.JSONWrite(w, 200, map[string]any{"data": result})
}
func (h *Handlers) SaveDeliveryRate(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	var v deliveryRate
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2000)).Decode(&v) != nil || len(strings.TrimSpace(v.Town)) < 2 || len(v.Town) > 100 || v.Fee < 0 || len(v.Estimate) < 2 || len(v.Estimate) > 120 {
		middleware.JSONWrite(w, 400, map[string]string{"error": "Enter a town, nonnegative fee and delivery estimate"})
		return
	}
	_, err := h.db.Pool.Exec(r.Context(), `INSERT INTO delivery_rates(town,fee,estimate) VALUES($1,$2,$3) ON CONFLICT(town) DO UPDATE SET fee=EXCLUDED.fee,estimate=EXCLUDED.estimate`, strings.TrimSpace(v.Town), v.Fee, v.Estimate)
	if err != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Could not save rate"})
		return
	}
	middleware.JSONWrite(w, 200, map[string]bool{"success": true})
}

func (h *Handlers) AdminCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	rows, err := h.db.Pool.Query(r.Context(), `SELECT jsonb_build_object('id',id,'title',name,'slug',slug,'price',price_minor/100,'stockCount',stock_count,'inStock',in_stock,'published',published,'brand',brand,'category',category,'shortDescription',short_description,'featuredImage',featured_image,'warranty',warranty,'specs',specs) FROM products ORDER BY name`)
	if err != nil {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Catalog unavailable"})
		return
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			middleware.JSONWrite(w, 500, map[string]string{"error": "Catalog read failed"})
			return
		}
		out = append(out, json.RawMessage(raw))
	}
	if rows.Err() != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Catalog read failed"})
		return
	}
	middleware.JSONWrite(w, 200, map[string]any{"data": out})
}
func (h *Handlers) AdminBulkCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.customerDB(w) {
		return
	}
	var data struct {
		Items []struct {
			ID              string `json:"id"`
			Price           int64  `json:"price"`
			Stock           int    `json:"stockCount"`
			Published       bool   `json:"published"`
			ManufactureDate string `json:"manufactureDate"`
		} `json:"items"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 100000)).Decode(&data) != nil || len(data.Items) == 0 || len(data.Items) > 100 {
		middleware.JSONWrite(w, 400, map[string]string{"error": "Choose 1–100 products"})
		return
	}
	for _, v := range data.Items {
		if v.Price < 0 || v.Price > 100000000 || v.Stock < 0 || v.Stock > 100000 || len(v.ManufactureDate) > 80 {
			middleware.JSONWrite(w, 400, map[string]string{"error": "Invalid price, stock or manufacture date"})
			return
		}
	}
	tx, err := h.db.Pool.Begin(r.Context())
	if err != nil {
		middleware.JSONWrite(w, 503, map[string]string{"error": "Could not begin update"})
		return
	}
	defer tx.Rollback(r.Context())
	for _, v := range data.Items {
		tag, err := tx.Exec(r.Context(), `UPDATE products SET price_minor=$1,stock_count=$2,in_stock=($2>0),published=$3,specs=jsonb_set(coalesce(specs,'{}'::jsonb),'{Manufacture date}',to_jsonb($4::text)),updated_at=now() WHERE id=$5`, v.Price*100, v.Stock, v.Published, v.ManufactureDate, v.ID)
		if err != nil || tag.RowsAffected() != 1 {
			middleware.JSONWrite(w, 400, map[string]string{"error": "No products saved. Check selected product IDs."})
			return
		}
	}
	if tx.Commit(r.Context()) != nil {
		middleware.JSONWrite(w, 500, map[string]string{"error": "Update failed; reload before retrying"})
		return
	}
	_ = h.cache.FlushPattern(r.Context(), "tyrelink:product*")
	middleware.JSONWrite(w, 200, map[string]bool{"success": true})
}
