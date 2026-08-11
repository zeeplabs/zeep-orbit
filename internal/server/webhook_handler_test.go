package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
)

// webhookTestSetup provisions zeep_system (idempotent — TestMain already
// did it) and creates one app + webhook for the public delivery handler
// tests. Returns the webhook row, its owning app id, and the plaintext
// token.
func webhookTestSetup(t *testing.T, method string) (dashboard.WebhookRow, string, string) {
	t.Helper()
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	ctx := context.Background()

	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO zeep_system.dashboard_users (email, password_hash, role) VALUES ($1, '', 'superadmin') RETURNING id`,
		"webhook-handler-"+suffix+"@example.com",
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	var appID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO zeep_system.apps (name, owner_id) VALUES ($1, $2) RETURNING id`,
		"webhook-handler-app-"+suffix, userID,
	).Scan(&appID)
	if err != nil {
		t.Fatalf("create test app: %v", err)
	}

	wh, token, err := dashboard.CreateWebhook(ctx, testPool, dashboard.CreateWebhookInput{
		AppID:         appID,
		Name:          "test webhook",
		Method:        method,
		EventTypePath: "eventType",
		EventIDPath:   "eventId",
		CreatedBy:     userID,
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	return wh, appID, token
}

func buildWebhookRouter(h *WebhookHandler) http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/hooks/{webhookId}/{token}", h.HandleWebhookDelivery)
	return r
}

func TestWebhookDelivery_MethodMismatchReturns404NoDeliveryLogged(t *testing.T) {
	wh, _, token := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	// Webhook is configured for POST; send GET instead.
	req := httptest.NewRequest(http.MethodGet, "/hooks/"+wh.ID+"/"+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on method mismatch, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no delivery logged on method mismatch, got %d", len(list))
	}
}

func TestWebhookDelivery_MissingTokenReturns401AndLogsInvalidToken(t *testing.T) {
	wh, _, _ := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	// chi's route pattern requires a non-empty {token} segment, so a truly
	// absent token can't be represented in the URL itself — "-" exercises
	// the same VerifyWebhookToken rejection path a genuinely missing token
	// would hit (empty/garbage presented value never matches a real hash).
	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/-", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing/invalid token, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "invalid_token" {
		t.Fatalf("expected 1 delivery logged with outcome=invalid_token, got %+v", list)
	}
}

func TestWebhookDelivery_WrongTokenReturns401AndLogsInvalidToken(t *testing.T) {
	wh, _, _ := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/completely-wrong-token", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong token, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "invalid_token" {
		t.Fatalf("expected 1 delivery logged with outcome=invalid_token, got %+v", list)
	}
}

func TestWebhookDelivery_MalformedBodyReturns400AndLogsMalformed(t *testing.T) {
	wh, _, token := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token, bytes.NewBufferString(`not-json{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d: %s", rec.Code, rec.Body.String())
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "malformed" {
		t.Fatalf("expected 1 delivery logged with outcome=malformed, got %+v", list)
	}
}

func TestWebhookDelivery_CaptureModeStoresSampleNoWrite(t *testing.T) {
	wh, appID, token := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token,
		bytes.NewBufferString(`{"eventType":"user.created","eventId":"evt-1","user":{"id":"u-1"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for capture-mode call, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := dashboard.GetWebhookByID(context.Background(), testPool, appID, wh.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if got.CapturedSample["eventType"] != "user.created" {
		t.Fatalf("expected captured_sample.eventType == 'user.created', got %v", got.CapturedSample)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "captured" {
		t.Fatalf("expected 1 delivery logged with outcome=captured, got %+v", list)
	}
}

func TestWebhookDelivery_SecondCaptureOverwritesSample(t *testing.T) {
	wh, appID, token := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	first := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token, bytes.NewBufferString(`{"a":1}`))
	first.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, first)
	if rec.Code != http.StatusOK {
		t.Fatalf("first capture: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token, bytes.NewBufferString(`{"b":2}`))
	second.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, second)
	if rec.Code != http.StatusOK {
		t.Fatalf("second capture: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, err := dashboard.GetWebhookByID(context.Background(), testPool, appID, wh.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if _, hasA := got.CapturedSample["a"]; hasA {
		t.Fatalf("expected the first sample to be overwritten, still has key 'a': %v", got.CapturedSample)
	}
	if got.CapturedSample["b"] != float64(2) {
		t.Fatalf("expected captured_sample.b == 2 after overwrite, got %v", got.CapturedSample)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 delivery log entries (one per call), got %d", len(list))
	}
}

func TestWebhookDelivery_VerificationChallengeEchoedBeforeCapture(t *testing.T) {
	wh, appID, token := webhookTestSetup(t, "POST")
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/hooks/"+wh.ID+"/"+token,
		bytes.NewBufferString(`{"type":"url_verification","token":"slack-verification-token","challenge":"3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for verification challenge, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Challenge != "3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P" {
		t.Fatalf("expected the challenge value echoed back verbatim, got %q", body.Challenge)
	}

	// Still in capture mode with no sample stored — the handshake must not
	// be mistaken for a real event to capture.
	got, err := dashboard.GetWebhookByID(context.Background(), testPool, appID, wh.ID)
	if err != nil {
		t.Fatalf("GetWebhookByID: %v", err)
	}
	if got.CapturedSample != nil {
		t.Fatalf("expected no captured sample from a verification challenge, got %v", got.CapturedSample)
	}

	list, err := dashboard.ListDeliveries(context.Background(), testPool, wh.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Outcome != "verification_challenge" {
		t.Fatalf("expected 1 delivery logged with outcome=verification_challenge, got %+v", list)
	}
	// Only the challenge value is persisted -- Slack's legacy verification
	// "token" field in the same payload has no reason to sit in
	// webhook_deliveries for 30 days.
	if _, hasToken := list[0].RawPayload["token"]; hasToken {
		t.Fatalf("expected the legacy verification token to NOT be persisted in raw_payload, got %+v", list[0].RawPayload)
	}
	if list[0].RawPayload["challenge"] != "3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P" {
		t.Fatalf("expected raw_payload to still record the challenge value, got %+v", list[0].RawPayload)
	}
}

func TestWebhookDelivery_UnknownWebhookIDReturns404(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	h := NewWebhookHandler(testPool, testReg)
	router := buildWebhookRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/hooks/00000000-0000-0000-0000-000000000000/whatever", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown webhook id, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestServerHooksRouteRegistered proves the route is wired into the real
// chi router built by New(), not just callable by invoking the handler
// directly (mirrors TestServerUpdateTablePolicyRouteRegistered's pattern in
// server_test.go). An unknown webhook id 404s from inside the handler
// itself — that 404 is the proof chi matched the route at all (a
// completely unregistered path would also 404, but chi's default not-found
// body differs from writeError's {"error":"..."} JSON shape, which we
// assert on here).
func TestServerHooksRouteRegistered(t *testing.T) {
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Skip("TEST_DATABASE_URL não configurado")
	}
	// Unlike newTestServer's nil-pool fixture (fine for routes that reject
	// on RequireAuth before ever touching the pool), this route reaches the
	// DB on every call — it needs the real testPool/testReg.
	s, err := New(testReg, testPool, 0)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/hooks/00000000-0000-0000-0000-000000000000/whatever", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST /hooks/...: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route registered, webhook not found)", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] == "" {
		t.Fatal("expected a JSON {\"error\":...} body from writeError, got none")
	}
}
