package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTrackingServer(t *testing.T) {
	server := NewServer()
	handler := server.Routes()

	// 1. Create order
	createReq := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"stepMs":100}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", createRec.Code)
	}

	var created map[string]any
	_ = json.NewDecoder(createRec.Body).Decode(&created)
	orderID := created["orderId"].(string)

	// 2. Short poll immediately -> returns 200 with current status
	shortReq := httptest.NewRequest("GET", "/orders/"+orderID+"/status", nil)
	shortRec := httptest.NewRecorder()
	handler.ServeHTTP(shortRec, shortReq)
	if shortRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on short poll, got %d", shortRec.Code)
	}

	// 3. Long poll when version already > sinceVersion (sinceVersion=0) -> returns immediately
	lpReq := httptest.NewRequest("GET", "/orders/"+orderID+"/status?sinceVersion=0&timeoutMs=1000", nil)
	lpRec := httptest.NewRecorder()
	handler.ServeHTTP(lpRec, lpReq)
	if lpRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on immediate long poll, got %d", lpRec.Code)
	}

	// 4. Long poll with high sinceVersion and short timeout -> should return 204 No Content
	timeoutReq := httptest.NewRequest("GET", "/orders/"+orderID+"/status?sinceVersion=999&timeoutMs=50", nil)
	timeoutRec := httptest.NewRecorder()
	handler.ServeHTTP(timeoutRec, timeoutReq)
	if timeoutRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on long poll timeout, got %d", timeoutRec.Code)
	}

	// 5. Long poll waiting for an advance
	waitChan := make(chan int)
	go func() {
		req := httptest.NewRequest("GET", "/orders/"+orderID+"/status?sinceVersion=1&timeoutMs=2000", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		waitChan <- rec.Code
	}()

	select {
	case code := <-waitChan:
		if code != http.StatusOK {
			t.Fatalf("expected 200 OK after advance, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("long poll did not receive update within timeout")
	}
}

