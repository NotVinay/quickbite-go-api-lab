package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Port             = ":8092"
	GatewayURL       = "http://localhost:8091"
	WebhookSecret    = "whsec_quickbite_demo_123"
	ToleranceSeconds = 5 * 60 // 5 minutes replay window
)

func hmacHex(secret, message string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

type Merchant struct {
	mu               sync.RWMutex
	orders           map[string]map[string]any
	processedEvents  map[string]bool
	received         atomic.Int64
	duplicates       atomic.Int64
	rejected         atomic.Int64
	flakyFailures    atomic.Int64
	seq              atomic.Int64
	client           *http.Client
}

func NewMerchant(flaky bool) *Merchant {
	m := &Merchant{
		orders:          make(map[string]map[string]any),
		processedEvents: make(map[string]bool),
		client:          &http.Client{Timeout: 5 * time.Second},
	}
	if flaky {
		m.flakyFailures.Store(2) // Fail first 2 attempts with 503
	}
	m.seq.Store(700)
	return m
}

// POST /orders
func (m *Merchant) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Amount *float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Amount == nil || *body.Amount <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "amount must be a positive number"})
		return
	}

	orderID := fmt.Sprintf("ord_%d", m.seq.Add(1))
	order := map[string]any{
		"orderId": orderID,
		"amount":  int64(*body.Amount),
		"status":  "PENDING_PAYMENT",
	}

	m.mu.Lock()
	m.orders[orderID] = order
	m.mu.Unlock()

	// Call QuickPay Gateway to create the payment
	reqPayload, _ := json.Marshal(map[string]any{
		"orderId":    orderID,
		"amount":     int64(*body.Amount),
		"webhookUrl": "http://localhost:8092/webhooks/quickpay",
	})

	resp, err := m.client.Post(GatewayURL+"/payments", "application/json", bytes.NewBuffer(reqPayload))
	if err != nil {
		m.mu.Lock()
		delete(m.orders, orderID)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "payment gateway unreachable"})
		return
	}
	defer resp.Body.Close()

	var gatewayResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&gatewayResp); err == nil {
		if pid, ok := gatewayResp["paymentId"].(string); ok {
			m.mu.Lock()
			order["paymentId"] = pid
			m.mu.Unlock()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(order)
}

// POST /webhooks/quickpay
// Implements:
// 1. Raw byte reading (preserves byte-exact body for signature check)
// 2. HMAC-SHA256 signature verification with constant-time equality
// 3. Timestamp replay-attack prevention (within 5 minutes tolerance)
// 4. Idempotency check via processedEvents set
func (m *Merchant) handleWebhook(w http.ResponseWriter, r *http.Request) {
	m.received.Add(1)

	// Read exact raw body bytes before decoding JSON!
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		m.rejected.Add(1)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Flaky mode simulation
	if m.flakyFailures.Load() > 0 {
		m.flakyFailures.Add(-1)
		log.Printf("[webhook] pretending to be temporarily down (503 Service Unavailable)")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "temporarily unavailable"})
		return
	}

	sigHeader := r.Header.Get("QuickPay-Signature")
	if sigHeader == "" || !strings.Contains(sigHeader, "t=") || !strings.Contains(sigHeader, "v1=") {
		m.rejected.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing or malformed QuickPay-Signature header"})
		return
	}

	// Parse t=<timestamp>,v1=<signature>
	var timestampSec int64 = -1
	var v1Signature string
	for _, part := range strings.Split(sigHeader, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "t=") {
			if n, err := strconv.ParseInt(part[2:], 10, 64); err == nil {
				timestampSec = n
			}
		} else if strings.HasPrefix(part, "v1=") {
			v1Signature = part[3:]
		}
	}

	if timestampSec == -1 || v1Signature == "" {
		m.rejected.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid signature header format"})
		return
	}

	// Verify timestamp tolerance to prevent replay attacks
	nowSec := time.Now().Unix()
	if math.Abs(float64(nowSec-timestampSec)) > ToleranceSeconds {
		m.rejected.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "timestamp outside tolerance window"})
		return
	}

	// Compute expected HMAC-SHA256 signature over "<timestamp>.<rawBody>"
	expectedSig := hmacHex(WebhookSecret, fmt.Sprintf("%d.%s", timestampSec, string(rawBody)))

	// Use constant-time byte comparison against timing attacks
	if !hmac.Equal([]byte(expectedSig), []byte(v1Signature)) {
		m.rejected.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid signature"})
		return
	}

	// Signature is valid! Parse the event payload
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			OrderID string `json:"orderId"`
			Status  string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &event); err != nil {
		m.rejected.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Idempotency: Ignore duplicate deliveries of the same event
	m.mu.Lock()
	if m.processedEvents[event.ID] {
		m.mu.Unlock()
		m.duplicates.Add(1)
		log.Printf("[webhook] duplicate event %s received -> ignored gracefully", event.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "duplicate ignored"})
		return
	}
	m.processedEvents[event.ID] = true

	// Apply state change to order
	if order, found := m.orders[event.Data.OrderID]; found {
		if event.Type == "payment.succeeded" {
			order["status"] = "PAID"
			log.Printf("[order] %s marked as PAID", event.Data.OrderID)
		} else if event.Type == "payment.failed" {
			order["status"] = "PAYMENT_FAILED"
			log.Printf("[order] %s marked as PAYMENT_FAILED", event.Data.OrderID)
		}
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"result": "processed"})
}

func (m *Merchant) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", m.handleCreateOrder)
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		m.mu.RLock()
		order, found := m.orders[id]
		m.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no such order"})
			return
		}
		_ = json.NewEncoder(w).Encode(order)
	})
	mux.HandleFunc("POST /webhooks/quickpay", m.handleWebhook)
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int64{
			"received":          m.received.Load(),
			"duplicatesIgnored": m.duplicates.Load(),
			"rejected":          m.rejected.Load(),
		})
	})
	return mux
}

func main() {
	flaky := flag.Bool("flaky", false, "fail first 2 webhook deliveries with 503 to observe retries")
	flag.Parse()

	merchant := NewMerchant(*flaky)
	flakyStr := ""
	if *flaky {
		flakyStr = "  [FLAKY mode]"
	}
	log.Printf("QuickBite Merchant App (Go) listening on http://localhost%s%s", Port, flakyStr)
	if err := http.ListenAndServe(Port, merchant.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

