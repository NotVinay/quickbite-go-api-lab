# Exercise 03 (Go) — Server-Sent Events (SSE)

**Port:** 8083  **You'll learn:** one-way server push over plain HTTP, the SSE wire format, resuming after a disconnect, browser EventSource

---

## The Story
Long polling solved latency, but each update still cost a brand-new HTTP request/response handshake. With **Server-Sent Events (SSE)**, the server keeps **one** persistent HTTP response open and streams events whenever state changes.

```mermaid
sequenceDiagram
    participant Browser
    participant Server
    Browser->>Server: GET /orders/ord_101/events
    Server-->>Browser: 200 text/event-stream (stays open)
    Server-->>Browser: id:1 PLACED
    Server-->>Browser: id:2 ACCEPTED
    Note over Browser,Server: Wi-Fi drops 📵
    Browser->>Server: GET .../events  (Last-Event-ID: 2)
    Server-->>Browser: id:3 PREPARING  (nothing lost)
    Server-->>Browser: id:4, id:5 DELIVERED → stream closes
```

---

## The SSE Wire Protocol

SSE is plain UTF-8 text sent over a persistent HTTP response. Each event consists of fields separated by newlines, ending with a **blank line** (`\n\n`):

```text
id: 1
event: status
data: {"orderId":"ord_101","status":"PLACED","version":1,"changedAt":1730000000000}

id: 2
event: status
data: {"orderId":"ord_101","status":"ACCEPTED","version":2,"changedAt":1730000005000}
```

### Core Architecture Concepts
* **EventSource**: Built right into every web browser (`const es = new EventSource('/orders/101/events')`) — zero client libraries or npm dependencies required.
* **Auto-Reconnect & `Last-Event-ID`**: If the Wi-Fi or mobile network drops, the browser automatically reconnects and includes the `Last-Event-ID: <id>` header. The server uses this to replay any missed events from its history buffer.
* **HTTP 204 No Content**: When the order reaches `DELIVERED` and all events are acknowledged, if the client attempts to reconnect, the server responds with **`204 No Content`**, which tells the browser's `EventSource` to permanently stop reconnecting.
* **Comments & Keep-Alive**: Lines starting with a colon (`: keep-alive\n\n`) are comments ignored by `EventSource`. Servers send them periodically to prevent proxies and load balancers from killing "idle" connections.
* **Go's `http.Flusher`**: Standard HTTP buffers responses before sending. Calling `w.(http.Flusher).Flush()` immediately forces each event frame onto the TCP socket.

---

## Think & Answer (Interview Questions)

### 1. SSE vs Long Polling: Count the HTTP requests for one order in each. What's the same, what's better?
* **Request Count**: Long polling takes **at least 5 separate HTTP requests** for 5 status changes (plus extra requests whenever the 30s polling timeout elapses). SSE takes **exactly 1 HTTP request** total.
* **What's the same**: Both are unidirectional (server-to-client push) over standard HTTP, both work over port 80/443 without special firewall rules, and both keep a connection held open.
* **What's better in SSE**: Far lower protocol overhead (no repeated request headers, TLS renegotiation, or cookies sent on every status update), zero connection churn, and automated browser reconnection with `Last-Event-ID` tracking built into `EventSource`.

### 2. Why does the server need to keep a history to support `Last-Event-ID`? What would you use in production?
* When a mobile device briefly drops network connectivity (e.g. entering an elevator) and reconnects with `Last-Event-ID: 2`, the server needs history to replay missed events (3, 4) so the client timeline doesn't skip steps.
* In production with multiple server instances, an in-memory list fails if the reconnect hits a different server. We would use a distributed append-only log with ID-based replay, such as **Redis Streams** (`XREAD ... ID`), **Apache Kafka**, or an append-only database table (`WHERE id > :lastEventId`).

### 3. Why send `: keep-alive` comments? Who is closing "idle" connections?
* Intermediate infrastructure (AWS ALBs, Nginx reverse proxies, cloud firewalls, NAT gateways) automatically terminate TCP connections that have no data flow for 30–60 seconds to clean up idle resources.
* SSE lines starting with a colon (`: keep-alive\n\n`) are comments ignored by the browser's `EventSource`, but they generate active TCP packets that reset proxy idle timers.

### 4. SSE is server $\to$ client only. How does the client send something back? Is that a problem for an order tracker? For a chat?
* To send data to the server, the client makes standard HTTP `POST`/`PATCH` requests.
* For an **order tracker**, this is ideal: 99% of data flows from server to client (status updates), while customer actions (cancel, add notes) are rare and fit clean REST endpoints.
* For **chat**, high-frequency two-way messages and typing indicators would require repeated HTTP POSTs, introducing noticeable overhead and latency; **WebSockets** are much better suited for bidirectional messaging.

### 5. Browsers allow only ~6 HTTP/1.1 connections per domain. What happens with 7 open tabs of the tracker? How does HTTP/2 change that?
* Under **HTTP/1.1**, each open SSE tab holds 1 of the 6 available TCP connection slots. Opening 6 tabs exhausts the browser limit; the 7th tab hangs waiting, and regular `fetch()` calls or image downloads to that domain are blocked.
* Under **HTTP/2 (and HTTP/3)**, connections are **multiplexed** over a single underlying TCP connection. Dozens of SSE streams and standard requests coexist concurrently over one connection without blocking.

### 6. Name two real products that could use SSE instead of WebSockets. (ChatGPT-style token streaming is one — why does it fit?)
* **ChatGPT / LLM Token Streaming**: The user sends one prompt via `POST`, and the server generates and streams tokens one by one for 10–30 seconds. The communication is strictly one-way, and SSE avoids the complexity and overhead of WebSockets.
* **Stock/Crypto Live Price Ticker (e.g. TradingView read-only widget) or Live Sports Scores (e.g. ESPN)**: Millions of viewers receiving real-time ticks from the server without ever sending messages back.

---

## How to Run

### 1. Start Server
```bash
go run ./03-sse/main.go
```
The server will start on `http://localhost:8083`.

### 2. Open in Browser
Open `http://localhost:8083` in your browser. Click **"Place Order"** to see live status updates stream in real time. Click **"Simulate Drop & Reconnect"** to test how `Last-Event-ID` replays missed events!

### 3. Run Automated Tests
```bash
go test -v ./03-sse/...
```
