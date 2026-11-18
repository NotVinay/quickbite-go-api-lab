package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// GIVEN — the QuickBite API server. It only routes HTTP to your feature functions; all Redis work
// lives in the feature files. You shouldn't need to edit this file (keeps merge conflicts away).
//
//   ./run.sh            -> http://localhost:8080   (demo page with a button for every feature)

func getPort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}

func StartApp() {
	pong, err := RedisStr("PING")
	if err != nil {
		fmt.Printf("Redis connection warning: %v\n", err)
	} else {
		fmt.Printf("Redis: %s from %s:%s\n", pong, getRedisHost(), getRedisPort())
	}

	_ = MainOrderPipeline.EnsureGroup()
	startRiderSimulator()

	mux := http.NewServeMux()
	mux.HandleFunc("/", safeHandler(routeHandler))

	port := getPort()
	fmt.Printf("QuickBite API on http://localhost:%s  -- open it in a browser\n", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Printf("Server failed: %v\n", err)
	}
}

func safeHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				msg := fmt.Sprint(rec)
				if strings.Contains(msg, "TODO") {
					sendJSON(w, http.StatusNotImplemented, map[string]any{"error": msg})
				} else {
					sendJSON(w, http.StatusInternalServerError, map[string]any{"error": "Server bug: " + msg})
				}
			}
		}()
		h(w, r)
	}
}

func routeHandler(w http.ResponseWriter, r *http.Request) {
	m := r.Method
	path := r.URL.Path
	p := strings.Split(strings.Trim(path, "/"), "/")

	// ---------- demo page ----------
	if path == "/" && m == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(DemoPageHTML))
		return
	}

	// ---------- Track A: menu cache ----------
	if len(p) == 3 && p[0] == "restaurants" && p[2] == "menu" && m == http.MethodGet {
		start := time.Now()
		res, err := GetMenu(p[1])
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if res == nil {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "No restaurant " + p[1]})
			return
		}
		cacheHeader := "MISS"
		if res.Hit {
			cacheHeader = "HIT"
		}
		w.Header().Set("X-Cache", cacheHeader)
		w.Header().Set("X-Time-Ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(res.JSON))
		return
	}
	if len(p) == 4 && p[0] == "restaurants" && p[2] == "menu" && m == http.MethodPut {
		body := readJSONBody(r)
		price := int(getNumber(body, "price", 0))
		ok, err := UpdatePrice(p[1], p[3], price)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if ok {
			sendJSON(w, http.StatusOK, map[string]any{"updated": true})
		} else {
			sendJSON(w, http.StatusNotFound, map[string]any{"updated": false})
		}
		return
	}

	// ---------- Track A: cart ----------
	if len(p) >= 2 && p[0] == "cart" {
		user := p[1]
		if len(p) == 3 && p[2] == "items" && m == http.MethodPost {
			body := readJSONBody(r)
			itemID, _ := body["itemId"].(string)
			qty := int(getNumber(body, "qty", 1))
			_, err := CartAddItem(user, itemID, qty)
			if err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			view, _ := CartView(user)
			sendJSON(w, http.StatusOK, view)
			return
		} else if m == http.MethodGet {
			view, err := CartView(user)
			if err != nil {
				sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			sendJSON(w, http.StatusOK, view)
			return
		} else if m == http.MethodDelete {
			_ = CartClear(user)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		sendJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
		return
	}

	// ---------- Track B: auth ----------
	if path == "/auth/otp" && m == http.MethodPost {
		body := readJSONBody(r)
		phone, _ := body["phone"].(string)
		code, err := RequestOtp(phone)
		if err != nil {
			if err == ErrCooldown {
				sendJSON(w, http.StatusTooManyRequests, map[string]any{"error": err.Error()})
				return
			}
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		// devCode is ONLY for this classroom demo. A real API never returns the OTP!
		sendJSON(w, http.StatusOK, map[string]any{"sent": true, "devCode": code})
		return
	}
	if path == "/auth/verify" && m == http.MethodPost {
		body := readJSONBody(r)
		phone, _ := body["phone"].(string)
		code, _ := body["code"].(string)
		res, err := VerifyOtp(phone, code)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		status := http.StatusUnauthorized
		if res.Status == "OK" {
			status = http.StatusOK
		} else if res.Status == "LOCKED" {
			status = http.StatusLocked
		}
		out := map[string]any{"status": res.Status}
		if res.Token != "" {
			out["token"] = res.Token
		}
		sendJSON(w, status, out)
		return
	}
	if path == "/auth/me" && m == http.MethodGet {
		sess, err := Session(bearerToken(r))
		if err != nil || sess == nil {
			sendJSON(w, http.StatusUnauthorized, map[string]any{"error": "Not logged in"})
			return
		}
		sendJSON(w, http.StatusOK, sess)
		return
	}
	if path == "/auth/logout" && m == http.MethodPost {
		ok, _ := Logout(bearerToken(r))
		sendJSON(w, http.StatusOK, map[string]any{"loggedOut": ok})
		return
	}

	// ---------- Track B: rate limit demo ----------
	if path == "/ratelimit-demo" && m == http.MethodPost {
		user := r.URL.Query().Get("user")
		if user == "" {
			user = "demo"
		}
		algo := r.URL.Query().Get("algo")
		var d RateDecision
		if algo == "fixed" {
			d = FixedWindow(user, RateLimit, RateWindowSeconds)
		} else {
			d = RateLimiterCheck(user)
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(d.Remaining, 10))
		if !d.Allowed {
			w.Header().Set("Retry-After", strconv.FormatInt(d.RetryAfterSeconds, 10))
		}
		status := http.StatusOK
		if !d.Allowed {
			status = http.StatusTooManyRequests
		}
		sendJSON(w, status, d)
		return
	}

	// ---------- Track C: flash sale ----------
	if len(p) >= 2 && p[0] == "sales" {
		sale := p[1]
		if len(p) == 3 && p[2] == "start" && m == http.MethodPost {
			body := readJSONBody(r)
			stock := int(getNumber(body, "stock", 100))
			_ = StartSale(sale, stock)
		} else if len(p) == 3 && p[2] == "buy" && m == http.MethodPost {
			body := readJSONBody(r)
			userID, _ := body["userId"].(string)
			mode := r.URL.Query().Get("mode")
			var result string
			if mode == "naive" {
				result = BuyNaive(sale, userID)
			} else {
				result = Buy(sale, userID)
			}
			status := http.StatusOK
			if result != "OK" {
				status = http.StatusConflict
			}
			sendJSON(w, status, map[string]any{"result": result})
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{
			"saleId":    sale,
			"remaining": SaleRemaining(sale),
			"buyers":    SaleBuyerCount(sale),
		})
		return
	}

	// ---------- Track D: leaderboard + riders ----------
	if len(p) >= 1 && p[0] == "leaderboard" && m == http.MethodGet {
		board := LeaderboardThisWeek()
		if len(p) == 2 {
			entry, err := RankOf(board, p[1])
			if err != nil || entry == nil {
				sendJSON(w, http.StatusNotFound, map[string]any{"error": "No orders this week for " + p[1]})
				return
			}
			sendJSON(w, http.StatusOK, entry)
			return
		}
		topN := 10
		if qTop := r.URL.Query().Get("top"); qTop != "" {
			if n, err := strconv.Atoi(qTop); err == nil {
				topN = n
			}
		}
		topEntries, err := TopLeaderboard(board, topN)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"board": board, "top": topEntries})
		return
	}
	if len(p) == 3 && p[0] == "riders" && p[2] == "location" && m == http.MethodPost {
		body := readJSONBody(r)
		lat := getFloat(body, "lat", 0)
		lng := getFloat(body, "lng", 0)
		_ = UpdateLocation(p[1], lat, lng)
		sendJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if path == "/riders/nearby" && m == http.MethodGet {
		lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
		lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
		km, _ := strconv.ParseFloat(r.URL.Query().Get("km"), 64)
		if km <= 0 {
			km = 5
		}
		limit := 5
		if qLim := r.URL.Query().Get("limit"); qLim != "" {
			if n, err := strconv.Atoi(qLim); err == nil {
				limit = n
			}
		}
		riders, err := Nearby(lat, lng, km, limit)
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		sendJSON(w, http.StatusOK, riders)
		return
	}

	// ---------- Track E: checkout + live updates ----------
	if path == "/checkout" && m == http.MethodPost {
		res := Checkout(bearerToken(r))
		sendJSON(w, res.Status, res.Body)
		return
	}
	if len(p) == 3 && p[0] == "orders" && p[2] == "live" && m == http.MethodGet {
		streamLive(w, r, p[1])
		return
	}

	sendJSON(w, http.StatusNotFound, map[string]any{"error": "No route for " + m + " " + path})
}

// SSE live updates using Pub/Sub
func streamLive(w http.ResponseWriter, r *http.Request, orderID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	listener := &sseListener{
		w:       w,
		flusher: flusher,
		closed:  r.Context().Done(),
	}

	_ = ListenLive(orderID, listener)
}

type sseListener struct {
	w       http.ResponseWriter
	flusher http.Flusher
	closed  <-chan struct{}
}

func (s *sseListener) OnMessage(jsonStr string) bool {
	select {
	case <-s.closed:
		return false
	default:
		_, err := fmt.Fprintf(s.w, "event: status\ndata: %s\n\n", jsonStr)
		if err != nil {
			return false
		}
		s.flusher.Flush()
		return true
	}
}

func (s *sseListener) OnIdle() bool {
	select {
	case <-s.closed:
		return false
	default:
		_, err := fmt.Fprintf(s.w, ": keep-alive\n\n")
		if err != nil {
			return false
		}
		s.flusher.Flush()
		return true
	}
}

func sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func readJSONBody(r *http.Request) map[string]any {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil || len(bodyBytes) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	_ = json.Unmarshal(bodyBytes, &out)
	if out == nil {
		out = make(map[string]any)
	}
	return out
}

func getNumber(m map[string]any, key string, def int64) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	default:
		return def
	}
}

func getFloat(m map[string]any, key string, def float64) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return def
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

// Pretend the 5 riders' phones send GPS every 10 s (they drift a little).
func startRiderSimulator() {
	go func() {
		warned := false
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						if !warned {
							fmt.Println("(rider simulator idle until UpdateLocation is implemented)")
							warned = true
						}
					}
				}()
				for id, start := range RiderStarts {
					jitter := (rand.Float64() - 0.5) * 0.004
					_ = UpdateLocation(id, start[0]+jitter, start[1]+jitter)
				}
			}()
			time.Sleep(10 * time.Second)
		}
	}()
}
