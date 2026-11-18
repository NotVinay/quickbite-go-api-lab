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

const Port = ":8083"

var lifecycle = []string{"PLACED", "ACCEPTED", "PREPARING", "OUT_FOR_DELIVERY", "DELIVERED"}

type EventData struct {
	OrderID   string `json:"orderId"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	ChangedAt int64  `json:"changedAt"`
}

type TrackedOrder struct {
	mu        sync.Mutex
	id        string
	history   []EventData
	listeners []chan struct{}
}

func NewTrackedOrder(id string) *TrackedOrder {
	o := &TrackedOrder{id: id}
	o.record(0)
	return o
}

func (o *TrackedOrder) record(step int) {
	entry := EventData{
		OrderID:   o.id,
		Status:    lifecycle[step],
		Version:   len(o.history) + 1,
		ChangedAt: time.Now().UnixMilli(),
	}
	o.history = append(o.history, entry)
}

func (o *TrackedOrder) advance() {
	o.mu.Lock()
	if len(o.history) < len(lifecycle) {
		o.record(len(o.history))
	}
	for _, ch := range o.listeners {
		close(ch)
	}
	o.listeners = nil
	o.mu.Unlock()
}

func (o *TrackedOrder) isDone() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.isDoneLocked()
}

func (o *TrackedOrder) isDoneLocked() bool {
	return len(o.history) == len(lifecycle)
}

// ---------- Server State ----------

type Server struct {
	mu     sync.RWMutex
	orders map[string]*TrackedOrder
	seq    atomic.Int64
}

func NewServer() *Server {
	s := &Server{orders: make(map[string]*TrackedOrder)}
	s.seq.Store(100)
	return s
}

func (s *Server) scheduleNextStep(order *TrackedOrder, stepMs int64) {
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
	_ = json.NewEncoder(w).Encode(map[string]string{"orderId": id})
}

// GET /orders/{id}/events
// Implements Server-Sent Events (SSE) with:
// - text/event-stream content type
// - Last-Event-ID header and ?lastEventId=N query param replay
// - 204 No Content when reconnecting to an already-delivered order
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Determine starting point from Last-Event-ID header or query param
	lastSent := 0
	lastHeader := r.Header.Get("Last-Event-ID")
	lastQuery := r.URL.Query().Get("lastEventId")
	if lastHeader != "" {
		if n, err := strconv.Atoi(lastHeader); err == nil {
			lastSent = n
		}
	} else if lastQuery != "" {
		if n, err := strconv.Atoi(lastQuery); err == nil {
			lastSent = n
		}
	}

	// If order is completed and client already received everything, return 204 No Content
	// This signals browser EventSource to permanently close reconnection attempts!
	order.mu.Lock()
	if order.isDoneLocked() && len(order.history) <= lastSent {
		order.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	order.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()

	for {
		var newEvents []EventData
		var done bool

		order.mu.Lock()
		for len(order.history) <= lastSent && !order.isDoneLocked() {
			ch := make(chan struct{})
			order.listeners = append(order.listeners, ch)
			order.mu.Unlock()

			select {
			case <-ch:
				order.mu.Lock()
			case <-time.After(15 * time.Second):
				// Keep-alive heartbeat comment
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
				order.mu.Lock()
			case <-ctx.Done():
				// Client disconnected
				order.mu.Lock()
				order.mu.Unlock()
				return
			}
		}

		if len(order.history) > lastSent {
			newEvents = append(newEvents, order.history[lastSent:]...)
		}
		done = order.isDoneLocked()
		order.mu.Unlock()

		for _, ev := range newEvents {
			lastSent++
			dataBytes, _ := json.Marshal(ev)
			// SSE Wire Format: id: <id>\nevent: <name>\ndata: <json>\n\n
			fmt.Fprintf(w, "id: %d\nevent: status\ndata: %s\n\n", lastSent, string(dataBytes))
			flusher.Flush()
		}

		if done && lastSent >= len(order.history) {
			break
		}
	}
}

// GET / -> Serve Live Tracker HTML page
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, trackerHTML)
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /orders", s.handleCreateOrder)
	mux.HandleFunc("GET /orders/{id}/events", s.handleEvents)
	return mux
}

func main() {
	server := NewServer()
	log.Printf("QuickBite SSE Server (Go) running on http://localhost%s (open in browser)", Port)
	if err := http.ListenAndServe(Port, server.Routes()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

const trackerHTML = `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>QuickBite Live Tracker (Go SSE)</title>
<style>
  body{font-family:system-ui,sans-serif;background:#0f0c29;color:#fff;max-width:640px;margin:32px auto;padding:0 16px}
  h1{background:linear-gradient(90deg,#ff512f,#f09819);-webkit-background-clip:text;color:transparent}
  button{border:0;border-radius:10px;padding:10px 16px;font-weight:700;cursor:pointer;margin-right:8px}
  #place{background:#00e676;color:#002}#drop{background:#ff1744;color:#fff}
  #conn{display:inline-block;margin-left:8px;padding:4px 10px;border-radius:999px;background:#555;font-size:13px}
  .on{background:#00b0ff!important}.off{background:#ff9100!important}
  ol{list-style:none;padding:0}li{margin:10px 0;padding:12px 14px;border-radius:12px;font-weight:600}
  .st-PLACED{background:#303f9f}.st-ACCEPTED{background:#00796b}.st-PREPARING{background:#fbc02d;color:#000}
  .st-OUT_FOR_DELIVERY{background:#e64a19}.st-DELIVERED{background:#2e7d32}
</style></head><body>
<h1>QuickBite Live Order Tracker (SSE)</h1>
<p>Uses standard browser <code>new EventSource('/orders/{id}/events')</code> with auto-reconnect and Last-Event-ID replay.</p>
<button id="place">Place Order</button>
<button id="drop" style="display:none">Simulate Drop & Reconnect</button>
<span id="conn" class="off">Disconnected</span>
<ol id="feed"></ol>
<script>
let es = null, orderId = null;
const feed = document.getElementById('feed'), conn = document.getElementById('conn'), drop = document.getElementById('drop');
function connect(id) {
  if (es) es.close();
  es = new EventSource('/orders/' + id + '/events');
  conn.textContent = 'Connected (SSE)'; conn.className = 'on';
  drop.style.display = 'inline-block';
  es.addEventListener('status', e => {
    const data = JSON.parse(e.data);
    const li = document.createElement('li');
    li.className = 'st-' + data.status;
    li.textContent = '#' + e.lastEventId + ' -> ' + data.status + ' (version ' + data.version + ')';
    feed.appendChild(li);
    if (data.status === 'DELIVERED') {
      es.close();
      conn.textContent = 'Delivered (Finished)';
      drop.style.display = 'none';
    }
  });
  es.onerror = () => {
    conn.textContent = 'Reconnecting...'; conn.className = 'off';
  };
}
document.getElementById('place').onclick = async () => {
  feed.innerHTML = '';
  const res = await fetch('/orders', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({stepMs:2000})});
  const data = await res.json();
  orderId = data.orderId;
  connect(orderId);
};
drop.onclick = () => {
  if (es) {
    es.close();
    conn.textContent = 'Dropped (reconnecting with Last-Event-ID)'; conn.className = 'off';
    setTimeout(() => connect(orderId), 1500);
  }
};
</script></body></html>`
