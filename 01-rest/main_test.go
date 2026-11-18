package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOrderAPI(t *testing.T) {
	server := NewServer()
	handler := server.Routes()

	goodOrder := `{"customerId":"cust_7","items":[{"itemId":"masala-dosa","qty":2},{"itemId":"gulab-jamun","qty":1}]}`

	t.Run("TODO(1) create order", func(t *testing.T) {
		// Valid order -> 201 Created
		req := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", w.Code, w.Body.String())
		}

		var order Order
		if err := json.NewDecoder(w.Body).Decode(&order); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if order.ID == "" || order.Status != StatusPlaced || order.Total != 337 {
			t.Fatalf("unexpected order values: %+v", order)
		}

		loc := w.Header().Get("Location")
		if loc != "/orders/"+order.ID {
			t.Fatalf("expected Location header /orders/%s, got %s", order.ID, loc)
		}

		// Validation errors -> 400 Bad Request
		badBodies := []struct {
			name string
			body string
		}{
			{"missing customerId", `{"items":[{"itemId":"masala-dosa","qty":1}]}`},
			{"empty items", `{"customerId":"c1","items":[]}`},
			{"unknown itemId", `{"customerId":"c1","items":[{"itemId":"pizza","qty":1}]}`},
			{"qty 0", `{"customerId":"c1","items":[{"itemId":"masala-dosa","qty":0}]}`},
			{"qty 25", `{"customerId":"c1","items":[{"itemId":"masala-dosa","qty":25}]}`},
			{"malformed JSON", `{oops`},
		}

		for _, tc := range badBodies {
			r := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("[%s] expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("TODO(2) get order", func(t *testing.T) {
		// Create an order first
		req := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var created Order
		_ = json.NewDecoder(w.Body).Decode(&created)

		// Get existing order
		getReq := httptest.NewRequest("GET", "/orders/"+created.ID, nil)
		getRec := httptest.NewRecorder()
		handler.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", getRec.Code)
		}

		// Non-existent order -> 404
		badReq := httptest.NewRequest("GET", "/orders/ord_999999", nil)
		badRec := httptest.NewRecorder()
		handler.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", badRec.Code)
		}
	})

	t.Run("TODO(3) list + filter + paginate", func(t *testing.T) {
		// Insert a few orders
		for i := 0; i < 4; i++ {
			r := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
		}

		// List with limit=2
		req := httptest.NewRequest("GET", "/orders?limit=2", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var page struct {
			Items  []Order `json:"items"`
			Total  int     `json:"total"`
			Limit  int     `json:"limit"`
			Offset int     `json:"offset"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&page)
		if len(page.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(page.Items))
		}
		if page.Total < 4 {
			t.Fatalf("expected total >= 4, got %d", page.Total)
		}

		// Offset comparison
		r0 := httptest.NewRequest("GET", "/orders?limit=1&offset=0", nil)
		rec0 := httptest.NewRecorder()
		handler.ServeHTTP(rec0, r0)
		var page0 struct {
			Items []Order `json:"items"`
		}
		_ = json.NewDecoder(rec0.Body).Decode(&page0)

		r1 := httptest.NewRequest("GET", "/orders?limit=1&offset=1", nil)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, r1)
		var page1 struct {
			Items []Order `json:"items"`
		}
		_ = json.NewDecoder(rec1.Body).Decode(&page1)

		if page0.Items[0].ID == page1.Items[0].ID {
			t.Fatalf("offset=1 should return different item than offset=0")
		}

		// Invalid queries -> 400
		invalids := []string{
			"/orders?status=BOGUS",
			"/orders?limit=abc",
			"/orders?limit=500",
			"/orders?offset=-5",
		}
		for _, q := range invalids {
			r := httptest.NewRequest("GET", q, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("[%s] expected 400, got %d", q, rec.Code)
			}
		}
	})

	t.Run("TODO(4) cancel with PATCH", func(t *testing.T) {
		// Create order
		r := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		var created Order
		_ = json.NewDecoder(rec.Body).Decode(&created)

		// Cancel it
		patchReq := httptest.NewRequest("PATCH", "/orders/"+created.ID, bytes.NewBufferString(`{"status":"CANCELLED"}`))
		patchRec := httptest.NewRecorder()
		handler.ServeHTTP(patchRec, patchReq)
		if patchRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", patchRec.Code, patchRec.Body.String())
		}

		// Attempt to mark DELIVERED (customer cannot) -> 400
		r2 := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, r2)
		var created2 Order
		_ = json.NewDecoder(rec2.Body).Decode(&created2)

		badPatch := httptest.NewRequest("PATCH", "/orders/"+created2.ID, bytes.NewBufferString(`{"status":"DELIVERED"}`))
		badPatchRec := httptest.NewRecorder()
		handler.ServeHTTP(badPatchRec, badPatch)
		if badPatchRec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", badPatchRec.Code)
		}

		// Advance order to PREPARING, then try cancelling -> 409 Conflict
		advanceReq := httptest.NewRequest("POST", "/admin/orders/"+created2.ID+"/advance", nil)
		advanceRec := httptest.NewRecorder()
		handler.ServeHTTP(advanceRec, advanceReq)
		if advanceRec.Code != http.StatusOK {
			t.Fatalf("expected 200 on advance, got %d", advanceRec.Code)
		}

		conflictPatch := httptest.NewRequest("PATCH", "/orders/"+created2.ID, bytes.NewBufferString(`{"status":"CANCELLED"}`))
		conflictRec := httptest.NewRecorder()
		handler.ServeHTTP(conflictRec, conflictPatch)
		if conflictRec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d", conflictRec.Code)
		}

		// Missing order -> 404
		missingPatch := httptest.NewRequest("PATCH", "/orders/ord_999999", bytes.NewBufferString(`{"status":"CANCELLED"}`))
		missingRec := httptest.NewRecorder()
		handler.ServeHTTP(missingRec, missingPatch)
		if missingRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", missingRec.Code)
		}

		// DELETE method not allowed -> 405
		delReq := httptest.NewRequest("DELETE", "/orders/"+created.ID, nil)
		delRec := httptest.NewRecorder()
		handler.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", delRec.Code)
		}
		if delRec.Header().Get("Allow") == "" {
			t.Fatalf("expected Allow header on 405 response")
		}
	})

	t.Run("TODO(5) idempotency", func(t *testing.T) {
		key := fmt.Sprintf("key-%d", time.Now().UnixNano())

		// First request -> 201 Created
		req1 := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		req1.Header.Set("Idempotency-Key", key)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec1.Code)
		}
		var o1 Order
		_ = json.NewDecoder(rec1.Body).Decode(&o1)

		// Second request with SAME key -> 200 OK + Idempotent-Replayed: true
		req2 := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		req2.Header.Set("Idempotency-Key", key)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec2.Code)
		}
		var o2 Order
		_ = json.NewDecoder(rec2.Body).Decode(&o2)

		if o1.ID != o2.ID {
			t.Fatalf("expected duplicate request to return identical order id %s, got %s", o1.ID, o2.ID)
		}
		if rec2.Header().Get("Idempotent-Replayed") != "true" {
			t.Fatalf("expected Idempotent-Replayed: true header")
		}

		// Different key -> new order
		key2 := fmt.Sprintf("key-%d", time.Now().UnixNano()+1)
		req3 := httptest.NewRequest("POST", "/orders", bytes.NewBufferString(goodOrder))
		req3.Header.Set("Idempotency-Key", key2)
		rec3 := httptest.NewRecorder()
		handler.ServeHTTP(rec3, req3)
		var o3 Order
		_ = json.NewDecoder(rec3.Body).Decode(&o3)
		if o1.ID == o3.ID {
			t.Fatalf("expected different order for different idempotency key")
		}
	})
}
