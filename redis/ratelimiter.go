package main

// TRACK B · Feature B1 — Rate limiter.                      Redis types: STRING counter, SORTED SET
//
// Problem: a buggy app build (or a bot) fires POST /orders 50 times a second. We allow each user
// 5 checkouts per minute. With 10 API servers behind a load balancer, the count can't live in
// one server's memory — every server must share it. That's Redis.
//
// Keys you own:   rl:fixed:{userId}:{windowNumber}    STRING counter
//                 rl:sliding:{userId}                 SORTED SET of request timestamps
//
// Try it:  redis-cli KEYS 'rl:*'     redis-cli ZRANGE rl:sliding:u1 0 -1 WITHSCORES

const (
	RateLimit         = 5
	RateWindowSeconds = 60
)

// RateDecision indicates if a request is allowed and rate limit metadata.
type RateDecision struct {
	Allowed           bool  `json:"allowed"`
	Remaining         int64 `json:"remaining"`
	RetryAfterSeconds int64 `json:"retryAfterSeconds"`
}

// RateLimiterCheck is what the API uses. Switch to FixedWindow(...) to compare behaviour.
func RateLimiterCheck(userID string) RateDecision {
	return SlidingWindow(userID, RateLimit, RateWindowSeconds*1000)
}

// TODO(1): Fixed window counter.
//   window = (current epoch seconds) / windowSeconds        e.g. minute number 29,812,345
//   key    = fmt.Sprintf("rl:fixed:%s:%d", userId, window)
//   count  = INCR key                     (atomic — 10 servers can do this at once safely)
//   if count == 1 -> EXPIRE key windowSeconds   (first hit in this window starts the clock)
//   allowed   = count <= limit
//   remaining = max(0, limit - count)
//   retryAfter = seconds until the window ends = windowSeconds - (nowSeconds % windowSeconds)
func FixedWindow(userID string, limit int, windowSeconds int) RateDecision {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Sliding window log — no "burst at the window boundary" problem (docs/TRACK-B.md Q1).
//   Keep one SORTED SET per user: member = unique request id, score = timestamp in ms.
//   On each request:
//     1. ZREMRANGEBYSCORE key 0 (now - windowMs)      drop requests older than the window
//     2. ZCARD key                                     how many are left = requests in the last windowMs
//     3. under the limit -> ZADD key now requestId, PEXPIRE key windowMs, allowed
//        at the limit    -> NOT allowed; retryAfter = when the OLDEST entry leaves the window
//                           (ZRANGE key 0 0 WITHSCORES gives the oldest)
//
//   These steps MUST run atomically, or two servers can both see "4" and both add -> 6 requests.
//   Do all of it in ONE Lua script (SlidingLua below) and run it with:
//     RedisEval(SlidingLua, []string{key}, now, windowMs, limit, requestId)
//   Make the script return {allowed (1/0), remaining, retryAfterMs}  -> Lua tables become a []any of int64.
const SlidingLua = `
-- KEYS[1] = key, ARGV = now(ms), windowMs, limit, requestId
-- write your script here
`

func SlidingWindow(userID string, limit int, windowMs int64) RateDecision {
	panic("TODO(2) not implemented yet")
}
