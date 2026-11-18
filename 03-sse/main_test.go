package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEServer(t *testing.T) {
	server := NewServer()
	handler := server.Routes()

	// 1. Create order with fast stepMs = 50ms
	createReq := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"stepMs":50}`))
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", createRec.Code)
	}

	var created map[string]string
	_ = json.NewDecoder(createRec.Body).Decode(&created)
	orderID := created["orderId"]

	// 2. Connect to SSE stream
	sseReq := httptest.NewRequest("GET", "/orders/"+orderID+"/events", nil)
	sseRec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(sseRec, sseReq)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("streaming timed out")
	}

	if sseRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", sseRec.Code)
	}
	if !strings.HasPrefix(sseRec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected text/event-stream Content-Type, got %s", sseRec.Header().Get("Content-Type"))
	}

	// Parse SSE events from output
	scanner := bufio.NewScanner(sseRec.Body)
	var eventIDs []string
	var statuses []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			eventIDs = append(eventIDs, strings.TrimPrefix(line, "id: "))
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var ev EventData
			_ = json.Unmarshal([]byte(data), &ev)
			statuses = append(statuses, ev.Status)
		}
	}

	if len(eventIDs) != 5 {
		t.Fatalf("expected 5 event IDs, got %d: %v", len(eventIDs), eventIDs)
	}
	if statuses[4] != "DELIVERED" {
		t.Fatalf("expected last status to be DELIVERED, got %s", statuses[4])
	}

	// 3. Reconnect with Last-Event-ID: 3 -> should only receive events 4 and 5
	recReq := httptest.NewRequest("GET", "/orders/"+orderID+"/events", nil)
	recReq.Header.Set("Last-Event-ID", "3")
	recRec := httptest.NewRecorder()
	handler.ServeHTTP(recRec, recReq)

	var reconnectedIDs []string
	scanner = bufio.NewScanner(recRec.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			reconnectedIDs = append(reconnectedIDs, strings.TrimPrefix(line, "id: "))
		}
	}
	if len(reconnectedIDs) != 2 || reconnectedIDs[0] != "4" || reconnectedIDs[1] != "5" {
		t.Fatalf("expected events [4 5], got %v", reconnectedIDs)
	}

	// 4. Reconnect with Last-Event-ID: 5 when order is DELIVERED -> 204 No Content
	endReq := httptest.NewRequest("GET", "/orders/"+orderID+"/events", nil)
	endReq.Header.Set("Last-Event-ID", "5")
	endRec := httptest.NewRecorder()
	handler.ServeHTTP(endRec, endReq)

	if endRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content after full delivery, got %d", endRec.Code)
	}

	// 5. Unknown order -> 404
	missingReq := httptest.NewRequest("GET", "/orders/ord_999999/events", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown order, got %d", missingRec.Code)
	}
}
