package main

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestWebSocketProtocol(t *testing.T) {
	// 1. Verify RFC 6455 Handshake Test Vector
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := acceptKey(key)
	if got != expected {
		t.Fatalf("acceptKey mismatch: expected %s, got %s", expected, got)
	}

	// 2. Test Frame roundtrip
	t.Run("Frame Serialization", func(t *testing.T) {
		payload := []byte("Hello WebSocket World!")
		var buf bytes.Buffer
		if err := writeFrame(&buf, OpcodeText, payload); err != nil {
			t.Fatalf("writeFrame failed: %v", err)
		}

		opcode, data, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("readFrame failed: %v", err)
		}
		if opcode != OpcodeText || string(data) != string(payload) {
			t.Fatalf("frame mismatch: got opcode %d, payload %s", opcode, string(data))
		}
	})

	// 3. Test In-Memory Hub Broadcast
	t.Run("Hub Chat Broadcast", func(t *testing.T) {
		hub := NewHub()

		// Create mock network connections using net.Pipe()
		serverConn1, clientConn1 := net.Pipe()
		serverConn2, clientConn2 := net.Pipe()
		defer serverConn1.Close()
		defer clientConn1.Close()
		defer serverConn2.Close()
		defer clientConn2.Close()

		c1 := &Client{user: "Alice", room: "room1", conn: serverConn1}
		c2 := &Client{user: "Bob", room: "room1", conn: serverConn2}

		// Drain clientConn1 in background so writes don't block
		go func() {
			for {
				if _, _, err := readFrame(clientConn1); err != nil {
					return
				}
			}
		}()

		// Read in background from clientConn2
		msgChan := make(chan string, 10)
		go func() {
			for {
				op, data, err := readFrame(clientConn2)
				if err != nil {
					return
				}
				if op == OpcodeText {
					msgChan <- string(data)
				}
			}
		}()

		hub.addClient(c1)
		hub.addClient(c2)

		// Broadcast a chat message from c1
		hub.broadcast("room1", map[string]any{
			"type": "message",
			"from": c1.user,
			"text": "Hello Bob!",
		}, nil)

		select {
		case msg := <-msgChan:
			var parsed map[string]any
			_ = json.Unmarshal([]byte(msg), &parsed)
			if parsed["type"] != "system" && parsed["type"] != "message" {
				t.Fatalf("unexpected message type: %v", parsed)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for message broadcast")
		}
	})
}
