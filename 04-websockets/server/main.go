package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	Port          = ":8084"
	WebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11" // RFC 6455 constant
)

// Opcodes defined in RFC 6455
const (
	OpcodeContinuation = 0x0
	OpcodeText         = 0x1
	OpcodeBinary       = 0x2
	OpcodeClose        = 0x8
	OpcodePing         = 0x9
	OpcodePong         = 0xA
)

// acceptKey computes Base64(SHA-1(key + GUID)) for the WebSocket handshake
func acceptKey(secWebSocketKey string) string {
	h := sha1.New()
	h.Write([]byte(strings.TrimSpace(secWebSocketKey) + WebSocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Client represents an active WebSocket connection
type Client struct {
	user string
	room string
	conn net.Conn
	mu   sync.Mutex
}

func (c *Client) sendText(text string) error {
	return c.sendFrame(OpcodeText, []byte(text))
}

func (c *Client) sendFrame(opcode int, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeFrame(c.conn, opcode, payload)
}

// ---------- Chat Hub & Rooms ----------

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*Client]bool
}

func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]map[*Client]bool),
	}
}

func (h *Hub) addClient(c *Client) {
	h.mu.Lock()
	if _, exists := h.rooms[c.room]; !exists {
		h.rooms[c.room] = make(map[*Client]bool)
	}
	h.rooms[c.room][c] = true
	h.mu.Unlock()

	h.broadcast(c.room, h.systemMessage(c.room, c.user+" joined"), nil)
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	if clients, exists := h.rooms[c.room]; exists {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.rooms, c.room)
		}
	}
	h.mu.Unlock()

	h.broadcast(c.room, h.systemMessage(c.room, c.user+" left"), nil)
}

func (h *Hub) broadcast(room string, msg map[string]any, except *Client) {
	data, _ := json.Marshal(msg)
	text := string(data)

	h.mu.RLock()
	clients := make([]*Client, 0)
	for c := range h.rooms[room] {
		if c != except {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range clients {
		_ = c.sendText(text)
	}
}

func (h *Hub) systemMessage(room, text string) map[string]any {
	h.mu.RLock()
	var online []string
	for c := range h.rooms[room] {
		online = append(online, c.user)
	}
	h.mu.RUnlock()
	sort.Strings(online)

	return map[string]any{
		"type":   "system",
		"text":   text,
		"online": online,
	}
}

// ---------- Frame Serialization (RFC 6455) ----------

// writeFrame writes a server->client frame (unmasked per RFC 6455)
func writeFrame(w io.Writer, opcode int, payload []byte) error {
	b0 := byte(0x80 | (opcode & 0x0F)) // FIN = 1
	n := len(payload)

	var header []byte
	if n < 126 {
		header = []byte{b0, byte(n)}
	} else if n < 65536 {
		header = make([]byte, 4)
		header[0] = b0
		header[1] = 126
		binary.BigEndian.PutUint16(header[2:4], uint16(n))
	} else {
		header = make([]byte, 10)
		header[0] = b0
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:10], uint64(n))
	}

	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

// readFrame reads and unmasks a client->server frame
func readFrame(r io.Reader) (int, []byte, error) {
	var buf bytes.Buffer
	messageOpcode := -1

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(r, header); err != nil {
			return 0, nil, err
		}

		fin := (header[0] & 0x80) != 0
		opcode := int(header[0] & 0x0F)
		masked := (header[1] & 0x80) != 0
		lenByte := int(header[1] & 0x7F)

		var length int64
		if lenByte < 126 {
			length = int64(lenByte)
		} else if lenByte == 126 {
			extended := make([]byte, 2)
			if _, err := io.ReadFull(r, extended); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint16(extended))
		} else {
			extended := make([]byte, 8)
			if _, err := io.ReadFull(r, extended); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint64(extended))
		}

		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(r, maskKey); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}

		// Unmask payload: payload[i] ^= mask[i % 4]
		if masked {
			for i := 0; i < len(payload); i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		// Control frames (Close, Ping, Pong) are never fragmented
		if opcode >= 0x8 {
			return opcode, payload, nil
		}

		if messageOpcode == -1 {
			messageOpcode = opcode
		}
		buf.Write(payload)

		if fin {
			return messageOpcode, buf.Bytes(), nil
		}
	}
}

// ---------- Connection Handler ----------

func handleConnection(conn net.Conn, hub *Hub) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reqLine, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	parts := strings.Fields(reqLine)
	if len(parts) < 2 {
		return
	}
	pathWithQuery := parts[1]

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
		colon := strings.Index(line, ":")
		if colon > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:colon]))
			val := strings.TrimSpace(line[colon+1:])
			headers[key] = val
		}
	}

	u, err := url.Parse(pathWithQuery)
	if err != nil {
		return
	}

	// Serve HTML UI on GET /
	if u.Path == "/" {
		resp := "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + chatHTML
		_, _ = conn.Write([]byte(resp))
		return
	}

	if u.Path != "/chat" {
		_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\n\r\nNot Found"))
		return
	}

	if strings.ToLower(headers["upgrade"]) != "websocket" {
		_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\nSend Upgrade: websocket"))
		return
	}

	user := u.Query().Get("user")
	room := u.Query().Get("order")
	if user == "" || room == "" {
		_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\nNeed ?order=...&user=..."))
		return
	}

	secKey := headers["sec-websocket-key"]
	accept := acceptKey(secKey)

	// Send HTTP 101 Switching Protocols
	handshake := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		return
	}

	client := &Client{
		user: user,
		room: room,
		conn: conn,
	}

	hub.addClient(client)
	defer hub.removeClient(client)

	log.Printf("+ %s@%s connected", user, room)

	// Main WebSocket Frame Loop
	for {
		opcode, payload, err := readFrame(reader)
		if err != nil {
			break // Client disconnected
		}

		switch opcode {
		case OpcodeText:
			var msg map[string]any
			if err := json.Unmarshal(payload, &msg); err != nil {
				continue
			}
			msgType, _ := msg["type"].(string)
			switch msgType {
			case "message":
				text, _ := msg["text"].(string)
				if strings.TrimSpace(text) != "" {
					hub.broadcast(client.room, map[string]any{
						"type": "message",
						"from": client.user,
						"text": text,
						"at":   time.Now().UnixMilli(),
					}, nil)
				}
			case "typing":
				hub.broadcast(client.room, map[string]any{
					"type": "typing",
					"from": client.user,
				}, client)
			default:
				_ = client.sendText(`{"type":"error","text":"unknown message type"}`)
			}

		case OpcodePing:
			_ = client.sendFrame(OpcodePong, payload)

		case OpcodeClose:
			_ = client.sendFrame(OpcodeClose, payload)
			return
		}
	}
}

func main() {
	hub := NewHub()
	listener, err := net.Listen("tcp", Port)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", Port, err)
	}
	defer listener.Close()

	log.Printf("QuickBite WebSocket Chat Server (Go) listening on http://localhost%s (open in 2 browser tabs)", Port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn, hub)
	}
}

const chatHTML = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>QuickBite Order Chat (WebSockets)</title>
<style>
  body{font-family:system-ui,sans-serif;background:#1a1a2e;color:#fff;max-width:600px;margin:24px auto;padding:0 16px}
  h2{color:#4ecca3}
  #messages{height:320px;overflow-y:auto;border:1px solid #162447;background:#0f3460;padding:12px;border-radius:8px;list-style:none}
  li{margin:8px 0;padding:8px 12px;border-radius:6px;max-width:80%}
  .msg-other{background:#1f4068}
  .msg-me{background:#e94560;margin-left:auto;text-align:right}
  .msg-system{background:transparent;color:#888;font-size:12px;text-align:center;max-width:100%}
  #typing{height:20px;color:#4ecca3;font-size:13px;font-style:italic}
  input,button{padding:10px 14px;border:0;border-radius:6px;font-size:14px}
  #text{width:75%;background:#fff;color:#000}
  #send{background:#4ecca3;color:#000;font-weight:700;cursor:pointer}
</style></head><body>
<h2>QuickBite Delivery Chat</h2>
<p id="info">Connecting...</p>
<ul id="messages"></ul>
<div id="typing"></div>
<form id="form" style="display:flex;gap:8px;margin-top:8px">
  <input id="text" placeholder="Type a message..." autocomplete="off"/>
  <button id="send" type="submit">Send</button>
</form>
<script>
const params = new URLSearchParams(location.search);
const user = params.get('user') || ('User_' + Math.floor(Math.random()*1000));
const order = params.get('order') || 'ord_42';
document.getElementById('info').textContent = 'Order: ' + order + ' | User: ' + user;

const ws = new WebSocket('ws://' + location.host + '/chat?order=' + order + '&user=' + user);
const msgs = document.getElementById('messages'), typingDiv = document.getElementById('typing'), input = document.getElementById('text');
let typingTimeout = null;

ws.onmessage = e => {
  const d = JSON.parse(e.data);
  if (d.type === 'message') {
    const li = document.createElement('li');
    li.className = d.from === user ? 'msg-me' : 'msg-other';
    li.innerHTML = '<strong>' + d.from + ':</strong> ' + d.text;
    msgs.appendChild(li);
    msgs.scrollTop = msgs.scrollHeight;
  } else if (d.type === 'typing') {
    typingDiv.textContent = d.from + ' is typing...';
    clearTimeout(typingTimeout);
    typingTimeout = setTimeout(() => typingDiv.textContent = '', 2000);
  } else if (d.type === 'system') {
    const li = document.createElement('li');
    li.className = 'msg-system';
    li.textContent = d.text + ' (Online: ' + d.online.join(', ') + ')';
    msgs.appendChild(li);
  }
};
input.oninput = () => ws.send(JSON.stringify({type:'typing'}));
document.getElementById('form').onsubmit = e => {
  e.preventDefault();
  if (!input.value.trim()) return;
  ws.send(JSON.stringify({type:'message', text:input.value}));
  input.value = '';
};
</script></body></html>`

