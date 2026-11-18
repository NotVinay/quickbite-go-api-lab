package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMerchantWebhook(t *testing.T) {
	m := NewMerchant(false)
	handler := m.Routes()

	// Seed an order
	m.orders["ord_1"] = map[string]any{
		"orderId": "ord_1",
		"status":  "PENDING_PAYMENT",
	}

	payload := `{"id":"evt_test_123","type":"payment.succeeded","data":{"orderId":"ord_1","status":"SUCCEEDED"}}`
	nowSec := time.Now().Unix()
	sig := fmt.Sprintf("t=%d,v1=%s", nowSec, hmacHex(WebhookSecret, fmt.Sprintf("%d.%s", nowSec, payload)))

	// 1. Valid webhook -> 200 OK + status changes to PAID
	req := httptest.NewRequest("POST", "/webhooks/quickpay", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("QuickPay-Signature", sig)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if m.orders["ord_1"]["status"] != "PAID" {
		t.Fatalf("expected order status PAID, got %v", m.orders["ord_1"]["status"])
	}

	// 2. Duplicate webhook delivery with same event ID -> 200 OK (idempotent ignore)
	recDup := httptest.NewRecorder()
	reqDup := httptest.NewRequest("POST", "/webhooks/quickpay", bytes.NewBufferString(payload))
	reqDup.Header.Set("Content-Type", "application/json")
	reqDup.Header.Set("QuickPay-Signature", sig)

	handler.ServeHTTP(recDup, reqDup)
	if recDup.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on duplicate, got %d", recDup.Code)
	}
	if m.duplicates.Load() != 1 {
		t.Fatalf("expected 1 duplicate ignored, got %d", m.duplicates.Load())
	}

	// 3. Tampered payload -> 401 Unauthorized
	tamperedPayload := `{"id":"evt_hacked","type":"payment.succeeded","data":{"orderId":"ord_1","status":"SUCCEEDED"}}`
	reqBad := httptest.NewRequest("POST", "/webhooks/quickpay", bytes.NewBufferString(tamperedPayload))
	reqBad.Header.Set("Content-Type", "application/json")
	reqBad.Header.Set("QuickPay-Signature", sig) // using signature of original payload
	recBad := httptest.NewRecorder()

	handler.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for tampered payload, got %d", recBad.Code)
	}

	// 4. Stale timestamp (replay attack 10 minutes ago) -> 401 Unauthorized
	staleSec := nowSec - 600
	staleSig := fmt.Sprintf("t=%d,v1=%s", staleSec, hmacHex(WebhookSecret, fmt.Sprintf("%d.%s", staleSec, payload)))
	reqStale := httptest.NewRequest("POST", "/webhooks/quickpay", bytes.NewBufferString(payload))
	reqStale.Header.Set("Content-Type", "application/json")
	reqStale.Header.Set("QuickPay-Signature", staleSig)
	recStale := httptest.NewRecorder()

	handler.ServeHTTP(recStale, reqStale)
	if recStale.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for stale timestamp, got %d", recStale.Code)
	}
}

