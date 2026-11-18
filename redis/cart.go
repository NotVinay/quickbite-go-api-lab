package main

import (
	"errors"
)

// TRACK A · Feature A2 — Shopping cart.                     Redis type: HASH
//
// Problem: the cart changes on every tap (+1 dosa, -1 coffee). Writing each tap to Postgres is
// heavy, and an abandoned cart should just disappear after a while.
//
// Keys you own:   cart:{userId}   HASH   field = itemId, value = quantity     expires 30 min after last change
//
//   POST   /cart/{userId}/items  {"itemId":"masala-dosa","qty":2}   (qty may be negative)
//   GET    /cart/{userId}
//   DELETE /cart/{userId}
//
// Try it:  redis-cli HGETALL cart:u1     redis-cli TTL cart:u1

const CartTTLSeconds = 30 * 60

func CartKey(userID string) string {
	return "cart:" + userID
}

// ErrInvalidCartItem is returned when an item doesn't exist or qty is 0.
var ErrInvalidCartItem = errors.New("invalid cart item or zero quantity")

// TODO(1): Add (or remove, if qty is negative) some quantity of an item.
//   - Unknown item (_, ok := FakeDbPriceOf(itemId); !ok) or qty == 0 -> return 0, ErrInvalidCartItem
//   - HINCRBY the field by qty. It returns the new quantity.
//   - New quantity <= 0 -> HDEL the field (nobody wants "-1 dosa")
//   - Reset the expiry of the WHOLE cart to CartTTLSeconds (EXPIRE) — it's a sliding expiry.
//   - Return the new quantity (0 if it was removed).
func CartAddItem(userID, itemID string, qty int) (int64, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Read the cart as itemId -> quantity.
//   HGETALL returns a flat list: [field1, value1, field2, value2, ...]. Empty cart -> empty map.
func CartItems(userID string) (map[string]int, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): Empty the cart (after checkout, or when the user taps "clear"). One command: DEL cart:{userId}.
func CartClear(userID string) error {
	panic("TODO(3) not implemented yet")
}

// GIVEN: the cart with prices and a total, for the API.
func CartView(userID string) (map[string]any, error) {
	items, err := CartItems(userID)
	if err != nil {
		return nil, err
	}

	lines := make([]map[string]any, 0, len(items))
	total := 0
	for itemID, qty := range items {
		price, _ := FakeDbPriceOf(itemID)
		lines = append(lines, map[string]any{
			"itemId": itemID,
			"qty":    qty,
			"price":  price,
		})
		total += price * qty
	}

	ttl, _ := RedisNum("TTL", CartKey(userID))

	return map[string]any{
		"userId":           userID,
		"items":            lines,
		"total":            total,
		"expiresInSeconds": ttl,
	}, nil
}
