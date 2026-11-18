package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Port          = ":8091"
	WebhookSecret = "whsec_quickbite_demo_123"
	MaxAttempts   = 5
)

func hmacHex(secret, message string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

func signatureHeader(body string, unixSeconds int64) string {
	message := fmt.Sprintf("%d.%s", unixSeconds, body)
	sig := hmacHex(WebhookSecret, message)
	return fmt.Sprintf("t=%d,v1=%s", unixSeconds, sig)
}

type DeliveryRecord struct {
	EventID string `json:"eventId"`
	Attempt int    `json:"attempt"`
	Result  any    `json:"result"`
	At      int64  `json:"at"`
}

type Gateway struct {
	duplicateMode bool
	mu            sync.RWMutex
	payments      map[string]map[string]any
	deliveries    []DeliveryRecord
	seq           atomic.Int64
	client        *http.Client
}

func NewGateway(duplicateMode bool) *Gateway {
	g := &Gateway{
		duplicateMode: duplicateMode,
		payments:      make(map[string]map[string]any),
		deliveries:    make([]DeliveryRecord, 0),
		client:        &http.Client{Timeout: 5 * time.Second},
	}
	g.seq.Store(5000)
	return g
}

func (g *Gateway) recordAttempt(eventID string, attempt int, result any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deliveries = append(g.deliveries, DeliveryRecord{
		EventID: eventID,
		Attempt: attempt,
		Result:  result,
		At:      time.Now().UnixMilli(),
	})
}

// POST /payments
func (g *Gateway) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OrderID    *string  `json:"orderId"`
		Amount     *float64 `json:"amount"`
		WebhookURL *string  `json:"webhookUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OrderID == nil || body.Amount == nil || body.WebhookURL == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "orderId, amount and webhookUrl are required"})
		return
	}

	paymentID := fmt.Sprintf("pay_%d", g.seq.Add(1))
	payment := map[string]any{
		"paymentId": paymentID,
		"orderId":   *body.OrderID,
		"amount":    int64(*body.Amount),
		"status":    "PENDING",
	}

	g.mu.Lock()
	g.payments[paymentID] = payment
	g.mu.Unlock()

	// Simulate bank processing taking 1.5 seconds, then deliver webhook
	go func() {
		time.Sleep(1500 * time.Millisecond)
		g.settle(payment, *body.WebhookURL)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(payment)
}

func (g *Gateway) settle(payment map[string]any, webhookURL string) {
	amount := payment["amount"].(int64)
	ok := amount <= 10000

	status := "FAILED"
	eventType := "payment.failed"
	if ok {
		status = "SUCCEEDED"
		eventType = "payment.succeeded"
	}

	g.mu.Lock()
	payment["status"] = status
	g.mu.Unlock()

	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes)
	eventID := fmt.Sprintf("evt_%x", randomBytes)

	event := map[string]any{
		"id":        eventID,
		"type":      eventType,
		"createdAt": time.Now().UnixMilli(),
		"data":      payment,
	}

	dataBytes, _ := json.Marshal(event)
	jsonStr := string(dataBytes)

	log.Printf("[bank] %s %s -> sending %s", payment["paymentId"], status, eventID)

	go g.deliverWithRetries(webhookURL, eventID, jsonStr)
	if g.duplicateMode {
		go g.deliverWithRetries(webhookURL, eventID, jsonStr)
	}
}

// deliverWithRetries sends the webhook with exponential backoff retry logic
func (g *Gateway) deliverWithRetries(url, eventID, jsonBody string) {
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		nowSec := time.Now().Unix()
		sig := signatureHeader(jsonBody, nowSec)

		req, err := http.NewRequest("POST", url, bytes.NewBufferString(jsonBody))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("QuickPay-Signature", sig)

		resp, err := g.client.Do(req)
		success := false
		if err != nil {
			g.recordAttempt(eventID, attempt, err.Error())
		} else {
			g.recordAttempt(eventID, attempt, resp.StatusCode)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				success = true
			}
			resp.Body.Close()
		}

		if success {
			log.Printf("[webhook] %s delivered successfully on attempt %d", eventID, attempt)
			return
		}

		if attempt < MaxAttempts {
			// Exponential backoff: 1s, 2s, 4s, 8s
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			time.Sleep(backoff)
		}
	}
	log.Printf("[webhook] %s permanently failed after %d attempts", eventID, MaxAttempts)
}

func (g *Gateway) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /payments", g.handleCreatePayment)
	mux.HandleFunc("GET /payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		g.mu.RLock()
		p, found := g.payments[id]
		g.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no such payment"})
			return
		}
		_ = json.NewEncoder(w).Encode(p)
	})
	mux.HandleFunc("GET /deliveries", func(w http.ResponseWriter, r *http.Request) {
		g.mu.RLock()
		del := make([]DeliveryRecord, len(g.deliveries))
		copy(del, g.deliveries)
		g.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(del)
	})
	return mux
}

func main() {
	dup := flag.Bool("duplicate", false, "deliver every event twice to test merchant idempotency")
	flag.Parse()

	gw := NewGateway(*dup)
	modeStr := ""
	if *dup {
		modeStr = "  [DUPLICATE mode]"
	}
	log.Printf("QuickPay Gateway (Go) listening on http://localhost%s%s", Port, modeStr)
	if err := http.ListenAndServe(Port, gw.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
