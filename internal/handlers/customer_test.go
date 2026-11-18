package handlers

import (
	"context"
	"encoding/json"
	"github.com/solveo/tyrelink/backend/internal/database"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCustomerStorageUnavailable(t *testing.T) {
	h := &Handlers{db: database.Connect(context.Background(), "")}
	for _, fn := range []func(*httptest.ResponseRecorder){
		func(w *httptest.ResponseRecorder) {
			h.GetReviews(w, httptest.NewRequest("GET", "/v1/products/test/reviews", nil))
		},
		func(w *httptest.ResponseRecorder) {
			h.CreateReview(w, httptest.NewRequest("POST", "/v1/products/test/reviews", strings.NewReader(`{"name":"Test","rating":5,"text":"Test review content"}`)))
		},
	} {
		w := httptest.NewRecorder()
		fn(w)
		if w.Code != 503 {
			t.Fatalf("expected503 got%d", w.Code)
		}
	}
}
func TestReviewModerationPersistence(t *testing.T) {
	dsn := os.Getenv("TYRELINK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated test database required")
	}
	db := database.Connect(context.Background(), dsn)
	defer db.Close()
	if !db.IsPostgres() {
		t.Fatal("database unavailable")
	}
	h := &Handlers{db: db}
	_, err := db.Pool.Exec(context.Background(), `INSERT INTO products(id,sku,brand,name,category,price_minor,published) VALUES('review-test','review-test','Test','Test tyre','car-tyres',100,true) ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Pool.Exec(context.Background(), `DELETE FROM products WHERE id='review-test'`)
	r := httptest.NewRequest("POST", "/v1/products/review-test/reviews", strings.NewReader(`{"name":"Test customer","rating":5,"text":"A useful product review for testing."}`))
	r.SetPathValue("id", "review-test")
	w := httptest.NewRecorder()
	h.CreateReview(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created map[string]string
	json.Unmarshal(w.Body.Bytes(), &created)
	read := func() string {
		r := httptest.NewRequest("GET", "/v1/products/review-test/reviews", nil)
		r.SetPathValue("id", "review-test")
		w := httptest.NewRecorder()
		h.GetReviews(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		return w.Body.String()
	}
	if strings.Contains(read(), "Test customer") {
		t.Fatal("pending review leaked publicly")
	}
	r = httptest.NewRequest("PUT", "/v1/admin/reviews/"+created["id"], strings.NewReader(`{"status":"approved"}`))
	r.SetPathValue("id", created["id"])
	w = httptest.NewRecorder()
	h.ModerateReview(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !strings.Contains(read(), "Test customer") {
		t.Fatal("approved review missing")
	}
	r = httptest.NewRequest("POST", "/v1/products/review-test/reviews", strings.NewReader(`{"name":"Test","rating":6,"text":"Invalid rating review"}`))
	r.SetPathValue("id", "review-test")
	w = httptest.NewRecorder()
	h.CreateReview(w, r)
	if w.Code != 400 {
		t.Fatal("invalid rating accepted")
	}
}
