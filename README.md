# QuickBite — API Design Lab (Go Edition)

A complete, production-grade Go port of the 7 API design exercises for the QuickBite food delivery system.

All exercises are built with clean, idiomatic Go, minimal external dependencies, and include full test suites (`go test ./go/...`).

---

## Exercises Overview

| # | Exercise | Protocol / Pattern | Key Concepts Learned |
|---|---|---|---|
| **01** | [`go/01-rest`](./01-rest/) | **REST API** | Resource modeling, `Idempotency-Key` caching, pagination, `PATCH` vs `DELETE`, `409 Conflict`. |
| **02** | [`go/02-polling`](./02-polling/) | **Short vs Long Polling** | `select` timeouts, Go channels for parked connections, measuring latency and wasted HTTP calls. |
| **03** | [`go/03-sse`](./03-sse/) | **Server-Sent Events** | `text/event-stream`, native browser `EventSource`, `Last-Event-ID` auto-replay, `204 No Content` stop. |
| **04** | [`go/04-websockets`](./04-websockets/) | **WebSockets (RFC 6455)** | `101 Switching Protocols` handshake, SHA-1 GUID hashing, frame encoding/masking, chat hub. |
| **05** | [`go/05-webhooks`](./05-webhooks/) | **Webhooks** | HMAC-SHA256 signatures, constant-time `hmac.Equal`, replay attack prevention, exponential backoff. |
| **06** | [`go/06-graphql`](./06-graphql/) | **GraphQL** | Schema definitions, resolvers, N+1 query problem, nested field querying, mutations. |
| **07** | [`go/07-grpc`](./07-grpc/) | **gRPC & Protobuf** | Unary, server-streaming, client-streaming RPCs, wire size savings vs JSON, context deadlines. |

---

## Quick Start: Run All Tests

To verify that all implementations pass their test suites:

```bash
go test ./go/...
```

---

## Directory Structure

```text
go/
├── 01-rest/
│   ├── main.go               # REST API server (port 8081)
│   ├── main_test.go          # Full unit test suite
│   └── README.md
├── 02-polling/
│   ├── server/main.go        # Tracking server (port 8082)
│   ├── server/server_test.go # Polling unit tests
│   ├── client/main.go        # CLI client with scorecard
│   └── README.md
├── 03-sse/
│   ├── main.go               # SSE server + Web UI (port 8083)
│   ├── main_test.go          # SSE streaming & replay tests
│   └── README.md
├── 04-websockets/
│   ├── server/main.go        # Raw TCP RFC 6455 server + UI (port 8084)
│   ├── server/server_test.go # Handshake & framing tests
│   ├── client/main.go        # Terminal CLI chat client
│   └── README.md
├── 05-webhooks/
│   ├── gateway/main.go       # QuickPay payment gateway (port 8091)
│   ├── merchant/main.go      # QuickBite webhook receiver (port 8092)
│   ├── merchant/merchant_test.go # Security & retry tests
│   └── README.md
├── 06-graphql/
│   ├── main.go               # GraphQL API + Playground (port 8086)
│   ├── main_test.go          # Schema, query, mutation tests
│   └── README.md
└── 07-grpc/
    ├── proto/delivery.proto  # Protobuf contract
    ├── pb/                   # Generated Go protobuf & gRPC stubs
    ├── server/main.go        # gRPC Delivery server (port 9090)
    ├── server/server_test.go # Unary, streaming & deadline tests
    ├── client/main.go        # gRPC client testing wire size & deadlines
    └── README.md
```

