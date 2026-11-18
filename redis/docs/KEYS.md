# Keyspace Registry — Fill this in BEFORE writing code (Go Edition)

Every key pattern or channel our Go application touches. One row per key pattern. If two rows could ever produce the same key, you have a collision bug.

**Conventions:** `object:id:field` with colons, lowercase, ids last. Every key either has a TTL or a documented sentence in "Why no TTL".

| Key pattern | Type | Example | TTL | Owner (track / name) | Written by | Read by | Why no TTL? |
|---|---|---|---|---|---|---|---|
| `menu:{restaurantId}` | STRING (JSON) | `menu:r1` | 60–70 s | A / | `GetMenu` | `GetMenu` | |
| `cart:{userId}` | HASH | `cart:9876543210` | 30 min sliding | A / | `CartAddItem` | `CartItems` | |
| `rl:fixed:{userId}:{window}` | STRING (counter) | `rl:fixed:9876543210:298123` | 60 s | B / | `FixedWindow` | `FixedWindow` | |
| `rl:sliding:{userId}` | ZSET | `rl:sliding:9876543210` | 60 s sliding | B / | `SlidingWindow` (Lua) | `SlidingWindow` (Lua) | |
| `otp:{phone}` | STRING | `otp:9876543210` | 120 s | B / | `RequestOtp` | `VerifyOtp` | |
| `otp:cooldown:{phone}` | STRING | `otp:cooldown:9876543210` | 30 s | B / | `RequestOtp` | `RequestOtp` | |
| `otp:attempts:{phone}` | STRING (counter) | `otp:attempts:9876543210` | 120 s | B / | `VerifyOtp` | `VerifyOtp` | |
| `session:{token}` | HASH | `session:d4e3f2...` | 30 min sliding | B / | `VerifyOtp` | `Session` | |
| `sale:{saleId}:stock` | STRING (counter) | `sale:biryani-8pm:stock` | n/a | C / | `StartSale`, `Buy` (Lua) | `SaleRemaining`, `Buy` (Lua) | Retained for audit; manually purged |
| `sale:{saleId}:buyers` | SET | `sale:biryani-8pm:buyers` | n/a | C / | `Buy` (Lua) | `Buy` (Lua), `SaleBuyerCount` | Retained for audit |
| `lock:{name}` | STRING | `lock:rider:rider-ravi` | 5000 ms | C / | `TryLock` | `Unlock` (Lua) | |
| `rider:{riderId}:order` | STRING | `rider:rider-ravi:order` | n/a | C / | `AssignRider` | `ReleaseRider` | Cleared on delivery completion |
| `lb:restaurants:{year}-W{week}` | ZSET | `lb:restaurants:2026-W47` | 8 days | D / | `RecordOrder` | `TopLeaderboard`, `RankOf` | |
| `riders:geo` | GEO (ZSET) | `riders:geo` | n/a | D / | `UpdateLocation` | `Nearby`, `GoOffline` | Persistent spatial index; lazy removal |
| `rider:{riderId}:alive` | STRING | `rider:rider-ravi:alive` | 30 s | D / | `UpdateLocation` | `Nearby` | |
| `order:{orderId}:status` | channel (Pub/Sub) | `order:ord_123:status` | n/a | E / | `PublishLive` | `ListenLive` | Channels store no data |
| `orders:stream` | STREAM | `orders:stream` | n/a (MAXLEN ~100k) | E / | `(p *OrderPipeline) Add` | `ReadNew`, `ClaimStuck` | Bounded by MAXLEN ~100000 |
| *(add your stretch-goal keys here)* | | | | | | | |

---

## Memory Estimate (To complete as a team)

Assume 1,000,000 daily active users, 200,000 orders/day, 20,000 restaurants, and 50,000 riders. Run `MEMORY USAGE <key>` on a real key in `redis-cli` to measure actual byte sizes.

| Key pattern | How many live at once? | Bytes each (`MEMORY USAGE`) | Total Memory |
|---|---|---|---|
| `menu:*` | ~20,000 | | |
| `session:*` | ~1,000,000 | | |
| `cart:*` | ~100,000 | | |
| `rl:sliding:*` | ~50,000 | | |
| `riders:geo` | 1 key (50,000 members) | | |
| `orders:stream` | ~100,000 entries | | |
| **Total** | | | |
