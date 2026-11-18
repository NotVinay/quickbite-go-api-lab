package main

// TRACK E · Integration I1 — Checkout.  EVERYONE's features meet here. Do this together, last.
//
//   POST /checkout     header Authorization: Bearer <token>
//
//   Customer app                     Checkout                          Redis
//   ─────────────                    ────────                          ─────
//   token  ───────────────────────►  1. OtpAuth.session(token)      ── session:{token}      (Track B)
//                                    2. RateLimiter.check(phone)    ── rl:sliding:{phone}   (Track B)
//                                    3. Cart.items(phone)           ── cart:{phone}         (Track A)
//                                    4. OrderPipeline.MAIN.add(...) ── orders:stream        (Track E)
//                                    5. Cart.clear(phone)
//                                    6. LiveUpdates.publish(PLACED) ── order:{id}:status    (Track E)
//   202 {orderId}  ◄───────────────
//
// The cart belongs to the logged-in user: userId = the session's phone number.

type CheckoutResponse struct {
	Status int            `json:"status"`
	Body   map[string]any `json:"body"`
}

func CheckoutError(status int, msg string) CheckoutResponse {
	return CheckoutResponse{
		Status: status,
		Body:   map[string]any{"error": msg},
	}
}

// TODO(1): Implement the checkout flow.
//   1. no session (or empty token)                  -> 401 CheckoutError(401, "Please log in")
//   2. rate limit not allowed                       -> 429, body also has "retryAfterSeconds"
//   3. empty cart                                   -> 400 CheckoutError(400, "Cart is empty")
//      items from more than one restaurant          -> 400 CheckoutError(400, "Items from multiple restaurants")
//                                                      (FakeDbRestaurantOf(itemId) tells you which)
//   4. orderId = "ord_" + 8 random hex/chars;  total = sum(price * qty)  (FakeDbPriceOf)
//      marshal items map to JSON string
//      add to the stream with fields: "orderId", "userId", "restaurantId", "items", "total"
//   5-6. clear the cart, publish "PLACED" on LiveUpdates
//   7. return CheckoutResponse: status 202, body: {"orderId": ..., "status": "PLACED", "total": ..., "live": "/orders/" + orderId + "/live"}
//   Order matters: why check the rate limit BEFORE reading the cart, and add to the stream BEFORE
//   clearing the cart? (docs/TRACK-E.md Q5)
func Checkout(token string) CheckoutResponse {
	panic("TODO(1) not implemented yet")
}
