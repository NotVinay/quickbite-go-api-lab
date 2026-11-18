package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const BaseURL = "http://localhost:8082"

type OrderStatus struct {
	OrderID   string `json:"orderId"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	ChangedAt int64  `json:"changedAt"`
}

type Scorecard struct {
	Requests int
	Wasted   int
	Missed   int
	Lags     []int64
}

func main() {
	mode := "short"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	intervalMs := int64(2000)
	if len(os.Args) > 2 {
		if n, err := strconv.ParseInt(os.Args[2], 10, 64); err == nil && n > 0 {
			intervalMs = n
		}
	}

	// 1. Create order
	resp, err := http.Post(BaseURL+"/orders", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		log.Fatalf("Cannot connect to %s. Start server first: go run ./go/02-polling/server/main.go", BaseURL)
	}
	defer resp.Body.Close()

	var created OrderStatus
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		log.Fatalf("failed to decode order response: %v", err)
	}

	fmt.Printf("Placed order %s -- tracking with %s polling\n\n", created.OrderID, mode)

	sc := &Scorecard{}
	startTime := time.Now()

	if mode == "long" {
		sc.runLongPoll(created.OrderID)
	} else {
		sc.runShortPoll(created.OrderID, time.Duration(intervalMs)*time.Millisecond)
	}

	totalSec := time.Since(startTime).Seconds()
	avgLag := int64(0)
	if len(sc.Lags) > 0 {
		sum := int64(0)
		for _, l := range sc.Lags {
			sum += l
		}
		avgLag = sum / int64(len(sc.Lags))
	}

	fmt.Printf("\n================ SCORECARD (%s) ================\n", mode)
	fmt.Printf("Total time          : %.1f s\n", totalSec)
	fmt.Printf("HTTP requests       : %d\n", sc.Requests)
	fmt.Printf("Wasted requests     : %d  (no news)\n", sc.Wasted)
	fmt.Printf("Avg notice delay    : %d ms  (how stale the UI was)\n", avgLag)
	fmt.Printf("Missed status steps : %d\n", sc.Missed)
}

func (sc *Scorecard) onUpdate(st OrderStatus, prevVersion int) {
	lag := time.Now().UnixMilli() - st.ChangedAt
	if prevVersion > 0 && st.Version > prevVersion+1 {
		skipped := st.Version - prevVersion - 1
		sc.Missed += skipped
		fmt.Printf("   !! skipped %d status change(s) between polls\n", skipped)
	}
	sc.Lags = append(sc.Lags, lag)
	fmt.Printf("%s  %-17s v%d   (noticed %4d ms late)\n",
		time.Now().Format("15:04:05"), st.Status, st.Version, lag)
}

func (sc *Scorecard) runShortPoll(orderID string, interval time.Duration) {
	lastVersion := 0
	client := &http.Client{Timeout: 5 * time.Second}

	for {
		sc.Requests++
		resp, err := client.Get(fmt.Sprintf("%s/orders/%s/status", BaseURL, orderID))
		if err != nil {
			log.Printf("request error: %v", err)
			time.Sleep(interval)
			continue
		}

		var st OrderStatus
		_ = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()

		if st.Version == lastVersion {
			sc.Wasted++
		} else {
			sc.onUpdate(st, lastVersion)
			lastVersion = st.Version
		}

		if st.Status == "DELIVERED" {
			break
		}
		time.Sleep(interval)
	}
}

func (sc *Scorecard) runLongPoll(orderID string) {
	lastVersion := 0
	client := &http.Client{Timeout: 40 * time.Second}

	for {
		sc.Requests++
		url := fmt.Sprintf("%s/orders/%s/status?sinceVersion=%d&timeoutMs=10000", BaseURL, orderID, lastVersion)
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("request error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusNoContent {
			// Timeout expired without change (204 No Content)
			resp.Body.Close()
			sc.Wasted++
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var st OrderStatus
			_ = json.NewDecoder(resp.Body).Decode(&st)
			resp.Body.Close()

			sc.onUpdate(st, lastVersion)
			lastVersion = st.Version

			if st.Status == "DELIVERED" {
				break
			}
		} else {
			resp.Body.Close()
		}
	}
}

