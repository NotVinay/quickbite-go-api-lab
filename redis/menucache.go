package main

// TRACK A · Feature A1 — Menu cache (cache-aside).          Redis type: STRING + TTL
//
// Problem: every app open loads a restaurant's menu. The DB query takes 300 ms (FakeDbGetMenu).
// Menus change maybe once a day. Reading them from Postgres 10,000 times a minute is waste.
//
// Keys you own:   menu:{restaurantId}   -> the menu as a JSON string, expires after ~60 s
//
//   GET /restaurants/{id}/menu                       -> header X-Cache: HIT or MISS
//   PUT /restaurants/{id}/menu/{itemId}  {"price":99} -> updates DB, invalidates cache
//
// Try it:  redis-cli GET menu:r1     redis-cli TTL menu:r1

const (
	MenuTTLSeconds       = 60
	MenuTTLJitterSeconds = 10
)

// MenuResult holds the JSON string and whether it was a cache HIT.
type MenuResult struct {
	JSON string
	Hit  bool
}

func MenuCacheKey(restaurantID string) string {
	return "menu:" + restaurantID
}

// TODO(1): Cache-aside read.
//   1. GET menu:{id}. Found? -> return &MenuResult{JSON: value, Hit: true}, nil
//   2. Not found -> FakeDbGetMenu(restaurantID)   (slow!)   -> nil means unknown restaurant: return nil, nil
//   3. Convert items to JSON (using json.Marshal); SET it with an expiry of MenuTTLSeconds + random(0..MenuTTLJitterSeconds)
//      (one command: SET key value EX seconds)
//   4. return &MenuResult{JSON: string(jsonBytes), Hit: false}, nil
//
//   Why the random jitter? See docs/TRACK-A.md, question 2.
func GetMenu(restaurantID string) (*MenuResult, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Write path. A restaurant owner changes a price.
//   1. Update the source of truth first: FakeDbUpdatePrice(...)  (false -> return false, nil)
//   2. Then DELETE menu:{id}, so the next read loads the fresh menu.
//   Why delete instead of SET-ing the new menu into the cache? See docs/TRACK-A.md, question 3.
func UpdatePrice(restaurantID, itemID string, newPrice int) (bool, error) {
	panic("TODO(2) not implemented yet")
}

// STRETCH(3): Stampede protection. When a hot key expires, 500 requests miss at the same
// moment and ALL of them hit the DB. Make only ONE of them rebuild the cache:
//   - on a miss, try  SET lock:menu:{id} 1 NX PX 2000
//   - won the lock  -> load from DB, SET the cache, DEL the lock
//   - lost the lock -> sleep 50 ms and try GetMenu again (max ~40 times)
//   The checker has a [STRETCH] test that fires 30 concurrent misses and counts DB reads.
func GetMenuProtected(restaurantID string) (*MenuResult, error) {
	panic("TODO(3) not implemented yet")
}
