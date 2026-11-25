package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/solveo/tyrelink/backend/internal/cache"
	"github.com/solveo/tyrelink/backend/internal/config"
	"github.com/solveo/tyrelink/backend/internal/database"
	"github.com/solveo/tyrelink/backend/internal/middleware"
)

func newTestHandlers(adminPass string) (*Handlers, *middleware.Middleware) {
	cfg := &config.Config{
		Env:           "test",
		Port:          "8080",
		AdminPassword: adminPass,
		SessionSecret: "test-session-secret-0123456789abcdef",
	}
	db := database.Connect(context.Background(), "")
	c := cache.New("")
	mw := middleware.New(cfg)
	return New(cfg, db, c, mw), mw
}

func TestAdminAuthRejectsDefaultBackdoorWhenConfigured(t *testing.T) {
	h, mw := newTestHandlers("SuperSecret2026!")

	// Attempt hardcoded "admin" password when real password differs -> MUST return 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewBufferString(`{"password":"admin"}`))
	h.AdminLogin(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong/default password, got %d", w.Code)
	}

	// Attempt configured password -> MUST return 200 with HMAC token
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewBufferString(`{"password":"SuperSecret2026!"}`))
	h.AdminLogin(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid password, got %d: %s", w2.Code, w2.Body.String())
	}

	var loginResp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &loginResp); err != nil {
		t.Fatal(err)
	}
	tok, _ := loginResp["token"].(string)
	if tok == "" {
		t.Fatal("expected non-empty token")
	}

	// Protected endpoint without token -> MUST reject 401
	protected := mw.RequireAdmin(h.AdminVerify)
	w3 := httptest.NewRecorder()
	protected(w3, httptest.NewRequest(http.MethodGet, "/v1/admin/verify", nil))
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %d", w3.Code)
	}

	// Protected endpoint with Bearer token -> MUST pass 200
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/v1/admin/verify", nil)
	req4.Header.Set("Authorization", "Bearer "+tok)
	protected(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("expected 200 authenticated, got %d", w4.Code)
	}
}

func TestOrderLeadLifecycleMemory(t *testing.T) {
	h, _ := newTestHandlers("test-pass")

	// 1. Reject invalid empty lead
	bad := httptest.NewRecorder()
	h.CreateOrderLead(bad, httptest.NewRequest(http.MethodPost, "/v1/order-leads", bytes.NewBufferString(`{"customerName":""}`)))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty lead, got %d", bad.Code)
	}

	// 2. Create valid lead
	body := `{"customerName":"Kamal Perera","phone1":"0771234567","address":"Colombo 05","items":[{"id":"tl-ceat-milaze-155","title":"CEAT Milaze","price":18500,"quantity":2}],"courierCharge":450}`
	w := httptest.NewRecorder()
	h.CreateOrderLead(w, httptest.NewRequest(http.MethodPost, "/v1/order-leads", bytes.NewBufferString(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 created, got %d: %s", w.Code, w.Body.String())
	}

	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	leadID, _ := created["leadId"].(string)
	if leadID == "" {
		t.Fatal("missing leadId")
	}

	// 3. Update status to dispatched
	updReq := httptest.NewRequest(http.MethodPut, "/v1/admin/leads/"+leadID, bytes.NewBufferString(`{"status":"dispatched","adminNotes":"Sent via PromptX"}`))
	updReq.SetPathValue("id", leadID)
	wUpd := httptest.NewRecorder()
	h.UpdateAdminLead(wUpd, updReq)
	if wUpd.Code != http.StatusOK {
		t.Fatalf("expected 200 update lead, got %d: %s", wUpd.Code, wUpd.Body.String())
	}

	// 4. Filter dispatched leads
	wList := httptest.NewRecorder()
	h.GetAdminLeads(wList, httptest.NewRequest(http.MethodGet, "/v1/admin/leads?status=dispatched&q=Kamal", nil))
	if wList.Code != http.StatusOK || !bytes.Contains(wList.Body.Bytes(), []byte(leadID)) {
		t.Fatalf("expected filtered lead in list, got %d: %s", wList.Code, wList.Body.String())
	}

	// 5. Delete lead
	delReq := httptest.NewRequest(http.MethodDelete, "/v1/admin/leads/"+leadID, nil)
	delReq.SetPathValue("id", leadID)
	wDel := httptest.NewRecorder()
	h.DeleteAdminLead(wDel, delReq)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 delete lead, got %d", wDel.Code)
	}
}

func TestProductFilteringAndCampaignsMemory(t *testing.T) {
	h, _ := newTestHandlers("test-pass")

	// Category + Search filter on /v1/products
	w := httptest.NewRecorder()
	h.GetProducts(w, httptest.NewRequest(http.MethodGet, "/v1/products?category=garage-equipment&q=Nitrogen", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 || len(resp.Data) != 1 {
		t.Fatalf("expected 1 nitrogen inflator match, got total=%d", resp.Total)
	}

	// Campaigns GET/PUT
	wCamp := httptest.NewRecorder()
	h.SaveCampaigns(wCamp, httptest.NewRequest(http.MethodPut, "/v1/admin/campaigns", bytes.NewBufferString(`{
		"flashSale":{"isActive":true,"startTime":"2026-11-01T00:00:00Z","endTime":"2026-12-01T00:00:00Z","productIds":["tl-ceat-milaze-155"]},
		"trending":{"isActive":true,"productIds":["tl-michelin-xm2-195"]}
	}`)))
	if wCamp.Code != http.StatusOK {
		t.Fatalf("expected 200 campaigns save, got %d: %s", wCamp.Code, wCamp.Body.String())
	}

	wGetCamp := httptest.NewRecorder()
	h.GetCampaigns(wGetCamp, httptest.NewRequest(http.MethodGet, "/v1/campaigns", nil))
	if wGetCamp.Code != http.StatusOK || !bytes.Contains(wGetCamp.Body.Bytes(), []byte("tl-ceat-milaze-155")) {
		t.Fatalf("expected persisted campaign product id, got %s", wGetCamp.Body.String())
	}
}

func TestCreateOrderLeadEnforcesServerPriceAndRejectsUnknownProduct(t *testing.T) {
	h, _ := newTestHandlers("test-pass")

	// Reject unknown product ID
	wBad := httptest.NewRecorder()
	h.CreateOrderLead(wBad, httptest.NewRequest(http.MethodPost, "/v1/order-leads", bytes.NewBufferString(
		`{"customerName":"Attacker","phone1":"0770000000","items":[{"id":"does-not-exist","price":1,"quantity":1}]}`,
	)))
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown product, got %d: %s", wBad.Code, wBad.Body.String())
	}

	// Forged client price=1, courierCharge=0 must be overwritten by authoritative catalog price (18500*2 + 450)
	wOk := httptest.NewRecorder()
	h.CreateOrderLead(wOk, httptest.NewRequest(http.MethodPost, "/v1/order-leads", bytes.NewBufferString(
		`{"customerName":"Kasun Silva","phone1":"0771112233","address":"Galle","items":[{"id":"tl-ceat-milaze-155","price":1,"quantity":2}],"subtotal":2,"courierCharge":0,"grandTotal":2}`,
	)))
	if wOk.Code != http.StatusCreated {
		t.Fatalf("expected 201 created, got %d: %s", wOk.Code, wOk.Body.String())
	}
	leads := h.db.GetMemoryLeads()
	if len(leads) == 0 || leads[0].Subtotal != 37000 || leads[0].CourierCharge != 450 || leads[0].GrandTotal != 37450 {
		t.Fatalf("expected authoritative total 37450 (37000+450), got %+v", leads)
	}
}

func TestUntrustedClientCannotSpoofXForwardedForToBypassLoginLimit(t *testing.T) {
	h, mw := newTestHandlers("SecretPass2026")
	guarded := mw.LoginRateLimit(h.AdminLogin)

	for i := 0; i < 12; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewBufferString(`{"password":"wrong"}`))
		req.RemoteAddr = "198.51.100.25:54321"
		req.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('1'+i)))
		guarded(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, w.Code)
		}
	}

	// 13th attempt from same untrusted public RemoteAddr with a fresh spoofed XFF header MUST hit 429
	wBlocked := httptest.NewRecorder()
	reqBlocked := httptest.NewRequest(http.MethodPost, "/v1/admin/login", bytes.NewBufferString(`{"password":"wrong"}`))
	reqBlocked.RemoteAddr = "198.51.100.25:54321"
	reqBlocked.Header.Set("X-Forwarded-For", "203.0.113.99")
	guarded(wBlocked, reqBlocked)
	if wBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 rate limit regardless of spoofed XFF, got %d", wBlocked.Code)
	}
}

