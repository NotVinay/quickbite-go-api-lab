package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Port       = ":8082"
	MaxWaitMs  = 30000
	DefaultWaitMs = 25000
)

var lifecycle = []string{"PLACED", "ACCEPTED", "PREPARING", "OUT_FOR_DELIVERY", "DELIVERED"}

// TrackedOrder represents an order moving through kitchen preparation
type TrackedOrder struct {
	mu        sync.Mutex
	id        string
	step      int
	version   int
	changedAt int64
	listeners []chan struct{}
}

func NewTrackedOrder(id string) *TrackedOrder {
	return &TrackedOrder{
		id:        id,
		step:      0,
		version:   1,
		changedAt: time.Now().UnixMilli(),
	}
}

func (o *TrackedOrder) isDone() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.step == len(lifecycle)-1
}

// advance moves the order to the next status and notifies all waiting long-pollers
func (o *TrackedOrder) advance() {
	o.mu.Lock()
	if o.step < len(lifecycle)-1 {
		o.step++
		o.version++
		o.changedAt = time.Now().UnixMilli()
	}
	currentStatus := lifecycle[o.step]
	currentVersion := o.version

	// Wake up all waiting long-polling goroutines by closing their channels
	for _, ch := range o.listeners {
		close(ch)
	}
	o.listeners = nil
	o.mu.Unlock()

	log.Printf("   [kitchen] %s -> %s (v%d)", o.id, currentStatus, currentVersion)
}

func (o *TrackedOrder) snapshot() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	return map[string]any{
		"orderId":   o.id,
		"status":    lifecycle[o.step],
		"version":   o.version,
		"changedAt": o.changedAt,
	}
}

// waitForUpdate waits until o.version > sinceVersion, or until timeout expires
func (o *TrackedOrder) waitForUpdate(sinceVersion int, timeout time.Duration) (map[string]any, bool) {
	o.mu.Lock()
	if o.version > sinceVersion {
		snap := map[string]any{
			"orderId":   o.id,
			"status":    lifecycle[o.step],
			"version":   o.version,
			"changedAt": o.changedAt,
		}
		o.mu.Unlock()
		return snap, true
	}

	ch := make(chan struct{})
	o.listeners = append(o.listeners, ch)
	o.mu.Unlock()

	select {
	case <-ch:
		// Notified of change
		return o.snapshot(), true
	case <-time.After(timeout):
		// Timed out with no change
		return nil, false
	}
}

// ---------- Server State ----------

type Server struct {
	mu     sync.RWMutex
	orders map[string]*TrackedOrder
	seq    atomic.Int64
}

func NewServer() *Server {
	s := &Server{
		orders: make(map[string]*TrackedOrder),
	}
	s.seq.Store(100)
	return s
}

func (s *Server) scheduleNextStep(order *TrackedOrder, stepMs int64) {
	// +/- 50% random jitter to simulate unpredictable kitchen prep times
	factor := 0.5 + rand.Float64()
	delay := time.Duration(float64(stepMs)*factor) * time.Millisecond

	time.AfterFunc(delay, func() {
		order.advance()
		if !order.isDone() {
			s.scheduleNextStep(order, stepMs)
		}
	})
}

// POST /orders
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StepMs *int64 `json:"stepMs"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	stepMs := int64(3000)
	if body.StepMs != nil && *body.StepMs > 0 {
		stepMs = *body.StepMs
	}

	id := fmt.Sprintf("ord_%d", s.seq.Add(1))
	order := NewTrackedOrder(id)

	s.mu.Lock()
	s.orders[id] = order
	s.mu.Unlock()

	s.scheduleNextStep(order, stepMs)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(order.snapshot())
}

// GET /orders/{id}/status
// Handles both Short Polling and Long Polling (when sinceVersion query param is present)
func (s *Server) handleOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	order, found := s.orders[id]
	s.mu.RUnlock()

	if !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unknown order " + id})
		return
	}

	q := r.URL.Query()
	sinceVersionStr := q.Get("sinceVersion")

	// 1. Short Polling: If no sinceVersion is provided, answer immediately
	if sinceVersionStr == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(order.snapshot())
		return
	}

	// 2. Long Polling: Hold connection until version > sinceVersion or timeout
	sinceVersion, err := strconv.Atoi(sinceVersionStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "sinceVersion must be an integer"})
		return
	}

	timeoutMs := int64(DefaultWaitMs)
	if timeoutStr := q.Get("timeoutMs"); timeoutStr != "" {
		if n, err := strconv.ParseInt(timeoutStr, 10, 64); err == nil && n > 0 {
			timeoutMs = n
		}
	}
	if timeoutMs > MaxWaitMs {
		timeoutMs = MaxWaitMs
	}

	snap, hasUpdate := order.waitForUpdate(sinceVersion, time.Duration(timeoutMs)*time.Millisecond)
	if hasUpdate {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(snap)
	} else {
		// 204 No Content informs client that timeout expired without any new status changes
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", s.handleCreateOrder)
	mux.HandleFunc("GET /orders/{id}/status", s.handleOrderStatus)
	return mux
}

func main() {
	server := NewServer()
	log.Printf("QuickBite Tracking Server (Go) running on http://localhost%s", Port)
	if err := http.ListenAndServe(Port, server.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

