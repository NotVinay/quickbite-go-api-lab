package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- Domain Types ----------

type Status string

const (
	StatusPlaced         Status = "PLACED"
	StatusPreparing      Status = "PREPARING"
	StatusOutForDelivery Status = "OUT_FOR_DELIVERY"
	StatusDelivered      Status = "DELIVERED"
	StatusCancelled      Status = "CANCELLED"
)

func isValidStatus(s Status) bool {
	switch s {
	case StatusPlaced, StatusPreparing, StatusOutForDelivery, StatusDelivered, StatusCancelled:
		return true
	default:
		return false
	}
}

type MenuItem struct {
	ItemID string `json:"itemId"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

type Item struct {
	ItemID string `json:"itemId"`
	Qty    int    `json:"qty"`
}

type Order struct {
	ID         string `json:"id"`
	CustomerID string `json:"customerId"`
	Items      []Item `json:"items"`
	Total      int    `json:"total"`
	Status     Status `json:"status"`
	CreatedAt  string `json:"createdAt"`
}

// Menu catalog
var menu = map[string]MenuItem{
	"paneer-tikka": {ItemID: "paneer-tikka", Name: "Paneer Tikka", Price: 249},
	"masala-dosa":  {ItemID: "masala-dosa", Name: "Masala Dosa", Price: 129},
	"veg-biryani":  {ItemID: "veg-biryani", Name: "Veg Biryani", Price: 219},
	"gulab-jamun":  {ItemID: "gulab-jamun", Name: "Gulab Jamun", Price: 79},
}

// Menu slice for ordering
var menuList = []MenuItem{
	menu["paneer-tikka"],
	menu["masala-dosa"],
	menu["veg-biryani"],
	menu["gulab-jamun"],
}

// ---------- In-Memory Store ----------

type OrderStore struct {
	mu              sync.RWMutex
	orders          map[string]Order
	orderKeys       []string // preserves insertion order for consistent pagination
	idempotencyKeys map[string]string
	seq             atomic.Int64
}

func NewOrderStore() *OrderStore {
	s := &OrderStore{
		orders:          make(map[string]Order),
		orderKeys:       make([]string, 0),
		idempotencyKeys: make(map[string]string),
	}
	s.seq.Store(1000)
	return s
}

// ---------- HTTP Helpers ----------

func sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, status int, message string) {
	sendJSON(w, status, map[string]string{"error": message})
}

// ---------- Handler Setup ----------

type Server struct {
	store *OrderStore
}

func NewServer() *Server {
	return &Server{store: NewOrderStore()}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /menu", s.handleMenu)
	mux.HandleFunc("POST /orders", s.handleCreateOrder)
	mux.HandleFunc("GET /orders", s.handleListOrders)
	mux.HandleFunc("GET /orders/{id}", s.handleGetOrder)
	mux.HandleFunc("PATCH /orders/{id}", s.handlePatchOrder)
	mux.HandleFunc("DELETE /orders/{id}", s.handleDeleteOrder)
	mux.HandleFunc("POST /admin/orders/{id}/advance", s.handleAdminAdvance)

	return mux
}

// GET /menu
func (s *Server) handleMenu(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, http.StatusOK, menuList)
}

// Request payload for creating an order
type createOrderRequest struct {
	CustomerID *string `json:"customerId"`
	Items      *[]struct {
		ItemID *string `json:"itemId"`
		Qty    *int    `json:"qty"`
	} `json:"items"`
}

// POST /orders
// Implements:
// - Validation (customerId non-blank, items non-empty, itemId in menu, qty 1..20)
// - Idempotency-Key support: returns 200 with original order if repeated
// - 201 Created with Location header on success
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey != "" {
		s.store.mu.RLock()
		orderID, exists := s.store.idempotencyKeys[idemKey]
		if exists {
			order, found := s.store.orders[orderID]
			s.store.mu.RUnlock()
			if found {
				w.Header().Set("Idempotent-Replayed", "true")
				sendJSON(w, http.StatusOK, order)
				return
			}
		} else {
			s.store.mu.RUnlock()
		}
	}

	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "malformed JSON body")
		return
	}

	if req.CustomerID == nil || strings.TrimSpace(*req.CustomerID) == "" {
		sendError(w, http.StatusBadRequest, "customerId is required and cannot be blank")
		return
	}

	if req.Items == nil || len(*req.Items) == 0 {
		sendError(w, http.StatusBadRequest, "items must be a non-empty list")
		return
	}

	var validatedItems []Item
	total := 0
	for _, item := range *req.Items {
		if item.ItemID == nil {
			sendError(w, http.StatusBadRequest, "each item must have an itemId")
			return
		}
		menuItem, exists := menu[*item.ItemID]
		if !exists {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("unknown or invalid itemId: %s", *item.ItemID))
			return
		}
		if item.Qty == nil {
			sendError(w, http.StatusBadRequest, "each item must have a qty")
			return
		}
		qty := *item.Qty
		if qty < 1 || qty > 20 {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("qty must be between 1 and 20, got %d", qty))
			return
		}
		validatedItems = append(validatedItems, Item{ItemID: *item.ItemID, Qty: qty})
		total += menuItem.Price * qty
	}

	id := fmt.Sprintf("ord_%d", s.store.seq.Add(1))
	order := Order{
		ID:         id,
		CustomerID: *req.CustomerID,
		Items:      validatedItems,
		Total:      total,
		Status:     StatusPlaced,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	s.store.mu.Lock()
	s.store.orders[id] = order
	s.store.orderKeys = append(s.store.orderKeys, id)
	if idemKey != "" {
		s.store.idempotencyKeys[idemKey] = id
	}
	s.store.mu.Unlock()

	w.Header().Set("Location", "/orders/"+id)
	sendJSON(w, http.StatusCreated, order)
}

// GET /orders/{id}
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.store.mu.RLock()
	order, found := s.store.orders[id]
	s.store.mu.RUnlock()

	if !found {
		sendError(w, http.StatusNotFound, "Order not found")
		return
	}
	sendJSON(w, http.StatusOK, order)
}

// GET /orders?status=PLACED&limit=10&offset=0
func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Parse optional status filter
	statusStr := q.Get("status")
	var filterStatus *Status
	if statusStr != "" {
		st := Status(statusStr)
		if !isValidStatus(st) {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("unknown status: %s", statusStr))
			return
		}
		filterStatus = &st
	}

	// Parse limit (default: 10, max: 50)
	limit := 10
	if limitStr := q.Get("limit"); limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer, got %s", limitStr))
			return
		}
		if n < 1 || n > 50 {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and 50, got %d", n))
			return
		}
		limit = n
	}

	// Parse offset (default: 0)
	offset := 0
	if offsetStr := q.Get("offset"); offsetStr != "" {
		n, err := strconv.Atoi(offsetStr)
		if err != nil {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("offset must be an integer, got %s", offsetStr))
			return
		}
		if n < 0 {
			sendError(w, http.StatusBadRequest, fmt.Sprintf("offset cannot be negative, got %d", n))
			return
		}
		offset = n
	}

	s.store.mu.RLock()
	var filtered []Order
	for _, key := range s.store.orderKeys {
		order := s.store.orders[key]
		if filterStatus == nil || order.Status == *filterStatus {
			filtered = append(filtered, order)
		}
	}
	s.store.mu.RUnlock()

	total := len(filtered)
	fromIndex := min(offset, total)
	toIndex := min(fromIndex+limit, total)
	page := filtered[fromIndex:toIndex]
	if page == nil {
		page = []Order{}
	}

	sendJSON(w, http.StatusOK, map[string]any{
		"items":  page,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// PATCH /orders/{id}
// Body: {"status":"CANCELLED"}
func (s *Server) handlePatchOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == nil || strings.TrimSpace(*body.Status) == "" {
		sendError(w, http.StatusBadRequest, "status is missing or not a string")
		return
	}

	newStatus := Status(*body.Status)
	if !isValidStatus(newStatus) {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("unknown status: %s", *body.Status))
		return
	}

	// Customers may only CANCEL orders
	if newStatus != StatusCancelled {
		sendError(w, http.StatusBadRequest, "can only CANCEL the orders")
		return
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	order, found := s.store.orders[id]
	if !found {
		sendError(w, http.StatusNotFound, fmt.Sprintf("Order %s not found", id))
		return
	}

	// Can only cancel if order is still PLACED
	if order.Status != StatusPlaced {
		sendError(w, http.StatusConflict, fmt.Sprintf("cannot cancel an order that is %s", order.Status))
		return
	}

	order.Status = StatusCancelled
	s.store.orders[id] = order

	sendJSON(w, http.StatusOK, order)
}

// DELETE /orders/{id}
// 405 Method Not Allowed with Allow header
func (s *Server) handleDeleteOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET, PATCH")
	sendError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
}

// POST /admin/orders/{id}/advance
// Advances order status: PLACED -> PREPARING -> OUT_FOR_DELIVERY -> DELIVERED
func (s *Server) handleAdminAdvance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	order, found := s.store.orders[id]
	if !found {
		sendError(w, http.StatusNotFound, "Order not found")
		return
	}

	var next Status
	switch order.Status {
	case StatusPlaced:
		next = StatusPreparing
	case StatusPreparing:
		next = StatusOutForDelivery
	default:
		next = StatusDelivered
	}

	if order.Status == StatusCancelled {
		next = StatusCancelled
	}

	order.Status = next
	s.store.orders[id] = order

	sendJSON(w, http.StatusOK, order)
}

// ---------- Main ----------

func main() {
	server := NewServer()
	port := ":8081"
	log.Printf("QuickBite Order API (Go) running on http://localhost%s", port)
	if err := http.ListenAndServe(port, server.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

