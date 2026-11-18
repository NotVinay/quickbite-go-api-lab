# Exercise 01 (Go) — REST: The QuickBite Order API

**Port:** 8081  **You'll learn:** resources vs actions, HTTP verbs, status codes, pagination, idempotency

---

## The Story
QuickBite's mobile app needs an API to place, view, list, and cancel food orders. We implement it using Go's standard library (`net/http`).

---

## Step 0 — API Design Contract

| User action | Method | Path | Success code | Failure codes |
|---|---|---|---|---|
| See the menu | GET | `/menu` | 200 OK | 500 Internal Server Error |
| Place an order | POST | `/orders` | 201 Created (+ Location header) | 400 Bad Request |
| View one order | GET | `/orders/{id}` | 200 OK | 404 Not Found |
| See my orders, 10 at a time | GET | `/orders?limit=10&offset=0` | 200 OK (page + total) | 400 Bad Request |
| Cancel an order | PATCH | `/orders/{id}` | 200 OK | 400 Bad Request, 404 Not Found, 409 Conflict |

---

## Step 1 — Implementation Overview in `main.go`

| Endpoint | Key High-Level Design Idea | Status Codes |
|---|---|---|
| `GET /menu` | Catalog fetch (safe, cacheable) | `200 OK` |
| `POST /orders` | Validate input $\to$ **400**; created $\to$ **201 + Location** header | `201 Created`, `400 Bad Request` |
| `GET /orders/{id}` | Missing $\to$ **404**, not 200 with `null` | `200 OK`, `404 Not Found` |
| `GET /orders?status=&limit=&offset=` | Offset pagination, input bounds validation (max 50) | `200 OK`, `400 Bad Request` |
| `PATCH /orders/{id}` | Business rule broken $\to$ **409 Conflict** (not 400!) | `200 OK`, `404`, `409 Conflict` |
| `Idempotency-Key` header | Safe retries for non-idempotent POST | `200 OK` (`Idempotent-Replayed: true`) |
| `DELETE /orders/{id}` | Orders are legal records $\to$ **405 Method Not Allowed** with `Allow` header | `405 Method Not Allowed` |

---

## Step 2 — Think & Answer (Interview Deep-Dive)

### 1. What's the difference between 400, 404, 409, and 422? Which did you use where, and why?
* **400 Bad Request**: Syntactically malformed JSON or invalid query/body parameters (e.g., missing `customerId`, non-integer `limit`/`offset`, unknown `itemId`).
* **404 Not Found**: The requested resource URI does not exist (e.g., `GET /orders/ord_999999`).
* **409 Conflict**: The request is well-formed, but violates current resource business state. Used in `PATCH /orders/{id}` when attempting to cancel an order already in `PREPARING` or later.
* **422 Unprocessable Entity**: Syntactically valid request with semantic/business validation errors (often preferred over 400 in strict APIs for field-level validation).

### 2. `GET`, `PUT`, `DELETE` are idempotent; `POST` isn't. What does that mean for a client that times out?
* If an idempotent call (`GET`, `PUT`, `DELETE`) times out or the TCP connection drops, the client can safely retry without side effects.
* If a non-idempotent `POST` times out, the client doesn't know if the order succeeded on the server before the connection died. Retrying could charge the customer twice or create a duplicate order. Adding an `Idempotency-Key` header allows safe retries by deduplicating requests on the server.

### 3. Why is there no `DELETE /orders/{id}`? (Hint: would Swiggy/DoorDash ever delete your order history?)
* Orders are transactional and legal records required for financial accounting, dispute resolution, tax audits, and analytics.
* Deleting rows destroys historical data and foreign key integrity. Instead of permanent deletion, order lifecycles use soft status transitions (e.g., `CANCELLED`).

### 4. Cancelling is modelled as `PATCH {"status":"CANCELLED"}`. Another option is `POST /orders/{id}/cancel`. Argue for each.
* **`PATCH {"status":"CANCELLED"}`**: Follows strict REST resource modeling. Orders are nouns/resources, and status is a mutable attribute partially updated via HTTP `PATCH`.
* **`POST /orders/{id}/cancel`**: Pragmatic, command/RPC-oriented sub-resource. Cancelling often triggers multi-step workflows (refund processing, alerting delivery riders, notifying kitchens) rather than just flipping a database column.

### 5. Offset pagination breaks when new orders arrive while a user scrolls. What is cursor pagination and how would `GET /orders` change?
* **Offset problem**: When new orders are inserted at the top while a user navigates from offset 0 to 10, items shift down and the user sees duplicate items on page 2. High offsets also degrade DB query performance (`OFFSET 100000`).
* **Cursor pagination**: The client receives an opaque pointer (e.g. timestamp or order ID of the last item seen) like `nextCursor: "ord_1015"`. The endpoint changes to `GET /orders?limit=10&cursor=ord_1015`, querying `WHERE created_at < :cursor ORDER BY created_at DESC LIMIT 10`, ensuring stable, duplicate-free scrolling in $O(1)$ indexed time.

### 6. Where would API versioning go — `/v1/orders`, a header, or a query param? Pick one and defend it.
* **URI Path (`/v1/orders`)**: Explicit, easily inspectable, and works seamlessly with standard API gateways, load balancers, and CDNs for path-based routing and caching without inspecting HTTP request headers. It also allows developers to test directly in a browser or curl without special header configuration.

---

## How to Run & Test

### Run the Server
```bash
go run ./01-rest/main.go
```
The server will start on `http://localhost:8081`.

### Run Automated Tests
```bash
go test -v ./01-rest/...
```

### Try it with `curl`

**1. Get Menu:**
```bash
curl -i http://localhost:8081/menu
```

**2. Place Order:**
```bash
curl -i -X POST http://localhost:8081/orders \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-uuid-1" \
  -d '{"customerId":"cust_1","items":[{"itemId":"masala-dosa","qty":2}]}'
```

**3. Test Idempotency (Repeat exact same request):**
```bash
curl -i -X POST http://localhost:8081/orders \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: my-unique-uuid-1" \
  -d '{"customerId":"cust_1","items":[{"itemId":"masala-dosa","qty":2}]}'
```
Notice `Idempotent-Replayed: true` and `200 OK`!

**4. Paginate Orders:**
```bash
curl -i "http://localhost:8081/orders?limit=10&offset=0"
```

**5. Cancel Order:**
```bash
curl -i -X PATCH http://localhost:8081/orders/ord_1001 \
  -H "Content-Type: application/json" \
  -d '{"status":"CANCELLED"}'
```
