package main

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The automatic checker. Needs Redis running, NOT the app.
//
//   ./run.sh check A        only Track A        (A, B, C, D, E or all)
//
// Tests call your feature functions directly and then look inside Redis to see what you stored.
// [STRETCH] tests are optional and never count as failures.

var (
	runID          = strconv.FormatInt(time.Now().UnixNano()%1_000_000, 36)
	passedCount    int
	failedCount    int
	stretchDone    int
	stretchTotal   int
)

func RunChecks(track string) {
	track = strings.ToUpper(track)
	if track == "" {
		track = "ALL"
	}

	pong, err := RedisStr("PING")
	if err != nil {
		fmt.Printf("Cannot connect to Redis at %s:%s: %v\n", getRedisHost(), getRedisPort(), err)
		os.Exit(1)
	}
	fmt.Printf("Redis: %s at %s:%s\n", pong, getRedisHost(), getRedisPort())

	if track == "ALL" || track == "A" {
		checkTrackA()
	}
	if track == "ALL" || track == "B" {
		checkTrackB()
	}
	if track == "ALL" || track == "C" {
		checkTrackC()
	}
	if track == "ALL" || track == "D" {
		checkTrackD()
	}
	if track == "ALL" || track == "E" {
		checkTrackE()
	}

	summaryLabel := "All tracks"
	if track != "ALL" {
		summaryLabel = "Track " + track
	}
	printSummary(summaryLabel)

	if failedCount > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}

func kitSection(title string) {
	fmt.Printf("\n-- %s --\n", title)
}

func kitTest(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			failedCount++
			fmt.Printf("  [FAIL] %s\n         %v\n", name, r)
		}
	}()
	fn()
	passedCount++
	fmt.Printf("  [PASS] %s\n", name)
}

func kitStretch(name string, fn func()) {
	stretchTotal++
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("  [STRETCH todo] %s  (%v)\n", name, r)
		}
	}()
	fn()
	stretchDone++
	fmt.Printf("  [STRETCH DONE] %s\n", name)
}

func kitInfo(msg string) {
	fmt.Printf("  [INFO] %s\n", msg)
}

func kitExpect(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}

func printSummary(what string) {
	fmt.Println()
	if failedCount == 0 {
		fmt.Printf("%d passed, %d failed  -> %s complete!\n", passedCount, failedCount, what)
	} else {
		fmt.Printf("%d passed, %d failed\n", passedCount, failedCount)
	}
	if stretchTotal > 0 {
		fmt.Printf("stretch goals: %d/%d\n", stretchDone, stretchTotal)
	}
}

func runConcurrently[T any](n int, fn func(i int) T) []T {
	start := make(chan struct{})
	results := make([]T, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		idx := i
		go func() {
			defer wg.Done()
			<-start
			results[idx] = fn(idx)
		}()
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()
	return results
}

// =====================================================================
func checkTrackA() {
	kitSection("A1 MenuCache -- cache-aside (STRING + TTL)")
	kitTest("first read is a MISS from the DB, second is a HIT from Redis", func() {
		_, _ = RedisCall("DEL", "menu:r3")
		before := atomic.LoadInt64(&FakeDbMenuReads)
		a, errA := GetMenu("r3")
		b, errB := GetMenu("r3")
		kitExpect(errA == nil && a != nil && !a.Hit, "first read should be a MISS")
		kitExpect(strings.Contains(a.JSON, "Butter Chicken"), "menu JSON looks wrong: "+a.JSON)
		kitExpect(errB == nil && b != nil && b.Hit, "second read should be a HIT")
		kitExpect(b.JSON == a.JSON, "the HIT returned different data than the MISS")
		reads := atomic.LoadInt64(&FakeDbMenuReads) - before
		kitExpect(reads == 1, fmt.Sprintf("DB was read %d times, expected 1", reads))
	})

	kitTest("a HIT is fast (no 300 ms DB call)", func() {
		_, _ = GetMenu("r3")
		start := time.Now()
		for i := 0; i < 20; i++ {
			_, _ = GetMenu("r3")
		}
		avgMs := time.Since(start).Milliseconds() / 20
		kitExpect(avgMs < 30, fmt.Sprintf("average HIT took %d ms", avgMs))
	})

	kitTest("menu:r3 has a TTL of 60-70 s (SET ... EX, with jitter)", func() {
		ttl, _ := RedisNum("TTL", "menu:r3")
		kitExpect(ttl > 50 && ttl <= 70, fmt.Sprintf("TTL is %d (-1 means you forgot the expiry)", ttl))
	})

	kitTest("unknown restaurant -> null, no crash", func() {
		res, err := GetMenu("r404")
		kitExpect(err == nil && res == nil, "expected nil")
	})

	kitTest("updatePrice changes the DB AND deletes the cached menu; next read sees the new price", func() {
		_, _ = GetMenu("r3")
		ok, err := UpdatePrice("r3", "dal-makhani", 239)
		kitExpect(err == nil && ok, "UpdatePrice returned false")
		exists, _ := RedisNum("EXISTS", "menu:r3")
		kitExpect(exists == 0, "menu:r3 is still cached -> users see the old price")
		r, err := GetMenu("r3")
		kitExpect(err == nil && r != nil && !r.Hit && strings.Contains(r.JSON, "239"), "new price not visible: "+r.JSON)
		_, _ = UpdatePrice("r3", "dal-makhani", 229)
	})

	kitTest("updatePrice for an unknown item -> false", func() {
		ok, err := UpdatePrice("r3", "pizza", 1)
		kitExpect(err == nil && !ok, "expected false")
	})

	kitStretch("30 concurrent misses on a cold key cause only ONE DB read (GetMenuProtected)", func() {
		_, _ = RedisCall("DEL", "menu:r4", "lock:menu:r4")
		before := atomic.LoadInt64(&FakeDbMenuReads)
		rs := runConcurrently(30, func(i int) *MenuResult {
			r, _ := GetMenuProtected("r4")
			return r
		})
		reads := atomic.LoadInt64(&FakeDbMenuReads) - before
		for _, r := range rs {
			kitExpect(r != nil && strings.Contains(r.JSON, "Podi"), "some callers got no menu")
		}
		kitExpect(reads == 1, fmt.Sprintf("DB was read %d times (stampede!)", reads))
	})

	kitSection("A2 Cart (HASH)")
	u := "chk-" + runID
	kitTest("HINCRBY: add 2 dosas, then 1 more -> 3", func() {
		_ = CartClear(u)
		q1, err1 := CartAddItem(u, "masala-dosa", 2)
		kitExpect(err1 == nil && q1 == 2, fmt.Sprintf("first add should return 2, got %d", q1))
		q2, err2 := CartAddItem(u, "masala-dosa", 1)
		kitExpect(err2 == nil && q2 == 3, fmt.Sprintf("second add should return 3, got %d", q2))
		val, _ := RedisStr("HGET", "cart:"+u, "masala-dosa")
		kitExpect(val == "3", "cart:"+u+" field masala-dosa should be 3")
	})

	kitTest("items() reads the whole hash", func() {
		_, _ = CartAddItem(u, "filter-coffee", 1)
		items, err := CartItems(u)
		kitExpect(err == nil, "CartItems error")
		kitExpect(items["masala-dosa"] == 3 && items["filter-coffee"] == 1 && len(items) == 2, fmt.Sprintf("items were %v", items))
	})

	kitTest("removing down to 0 deletes the field", func() {
		rem, err := CartAddItem(u, "masala-dosa", -5)
		kitExpect(err == nil && rem == 0, fmt.Sprintf("should return 0, got %d", rem))
		exists, _ := RedisNum("HEXISTS", "cart:"+u, "masala-dosa")
		kitExpect(exists == 0, "field still there with a negative/zero qty")
	})

	kitTest("every change resets the cart TTL to 30 min", func() {
		_, _ = RedisCall("EXPIRE", "cart:"+u, 100)
		_, _ = CartAddItem(u, "filter-coffee", 1)
		ttl, _ := RedisNum("TTL", "cart:"+u)
		kitExpect(ttl > 1790 && ttl <= 1800, fmt.Sprintf("TTL is %d", ttl))
	})

	kitTest("unknown item or qty 0 -> error", func() {
		_, err1 := CartAddItem(u, "pizza", 1)
		kitExpect(err1 != nil, "expected error for unknown item")
		_, err2 := CartAddItem(u, "filter-coffee", 0)
		kitExpect(err2 != nil, "expected error for zero qty")
	})

	kitTest("clear() removes the key", func() {
		_ = CartClear(u)
		exists, _ := RedisNum("EXISTS", "cart:"+u)
		items, _ := CartItems(u)
		kitExpect(exists == 0 && len(items) == 0, "cart still exists")
	})
}

// =====================================================================
func checkTrackB() {
	kitSection("B1 RateLimiter")
	kitTest("fixed window: 5 allowed (remaining 4..0), 6th refused", func() {
		u := "fx-" + runID
		for i := 1; i <= 5; i++ {
			d := FixedWindow(u, 5, 60)
			kitExpect(d.Allowed, fmt.Sprintf("request %d was refused", i))
			kitExpect(d.Remaining == int64(5-i), fmt.Sprintf("request %d: remaining should be %d, was %d", i, 5-i, d.Remaining))
		}
		d := FixedWindow(u, 5, 60)
		kitExpect(!d.Allowed, "6th request was allowed")
		kitExpect(d.RetryAfterSeconds >= 1 && d.RetryAfterSeconds <= 60, fmt.Sprintf("retryAfter should be 1..60, was %d", d.RetryAfterSeconds))
	})

	kitTest("fixed window: the counter key expires (no memory leak)", func() {
		u := "fx2-" + runID
		_ = FixedWindow(u, 5, 60)
		window := time.Now().Unix() / 60
		ttl, _ := RedisNum("TTL", fmt.Sprintf("rl:fixed:%s:%d", u, window))
		kitExpect(ttl > 0 && ttl <= 60, fmt.Sprintf("TTL of rl:fixed:%s:%d is %d", u, window, ttl))
	})

	kitTest("fixed window: 40 concurrent requests, limit 10 -> exactly 10 allowed", func() {
		u := "fx3-" + runID
		res := runConcurrently(40, func(i int) RateDecision {
			return FixedWindow(u, 10, 60)
		})
		okCount := 0
		for _, d := range res {
			if d.Allowed {
				okCount++
			}
		}
		kitExpect(okCount == 10, fmt.Sprintf("%d allowed", okCount))
	})

	kitTest("users don't share limits", func() {
		for i := 0; i < 6; i++ {
			_ = FixedWindow("noisy-"+runID, 5, 60)
		}
		d := FixedWindow("quiet-"+runID, 5, 60)
		kitExpect(d.Allowed, "a different user was refused")
	})

	kitTest("sliding window: limit 3 per 1 s -> 4th refused, allowed again after the window slides", func() {
		u := "sl-" + runID
		for i := 0; i < 3; i++ {
			kitExpect(SlidingWindow(u, 3, 1000).Allowed, fmt.Sprintf("request %d refused", i+1))
		}
		d := SlidingWindow(u, 3, 1000)
		kitExpect(!d.Allowed, "4th request allowed")
		kitExpect(d.RetryAfterSeconds == 1, fmt.Sprintf("retryAfter should be 1 s, was %d", d.RetryAfterSeconds))
		time.Sleep(1100 * time.Millisecond)
		kitExpect(SlidingWindow(u, 3, 1000).Allowed, "still refused after the window passed")
		t, _ := RedisStr("TYPE", "rl:sliding:"+u)
		kitExpect(t == "zset", "rl:sliding:"+u+" should be a sorted set")
	})

	kitTest("sliding window: 40 concurrent requests, limit 10 -> exactly 10 (atomic Lua)", func() {
		u := "sl2-" + runID
		res := runConcurrently(40, func(i int) RateDecision {
			return SlidingWindow(u, 10, 60000)
		})
		okCount := 0
		for _, d := range res {
			if d.Allowed {
				okCount++
			}
		}
		kitExpect(okCount == 10, fmt.Sprintf("%d allowed -- is the check+add atomic?", okCount))
	})

	kitSection("B2 OTP + sessions")
	phone := "90" + runID
	var code, token string

	kitTest("requestOtp stores a 6-digit code with a 120 s TTL", func() {
		_, _ = RedisCall("DEL", "otp:cooldown:"+phone)
		var err error
		code, err = RequestOtp(phone)
		kitExpect(err == nil, "RequestOtp error")
		matched, _ := regexp.MatchString(`^\d{6}$`, code)
		kitExpect(matched, "code should be 6 digits: "+code)
		stored, _ := RedisStr("GET", "otp:"+phone)
		kitExpect(stored == code, "otp:"+phone+" doesn't hold the code")
		ttl, _ := RedisNum("TTL", "otp:"+phone)
		kitExpect(ttl > 110 && ttl <= 120, fmt.Sprintf("TTL is %d", ttl))
	})

	kitTest("asking again within 30 s -> ErrCooldown", func() {
		_, err := RequestOtp(phone)
		kitExpect(err == ErrCooldown, fmt.Sprintf("expected ErrCooldown, got %v", err))
	})

	kitTest("wrong code -> WRONG_CODE; right code -> OK + token", func() {
		wrong := "000000"
		if code == "000000" {
			wrong = "111111"
		}
		resWrong, _ := VerifyOtp(phone, wrong)
		kitExpect(resWrong.Status == "WRONG_CODE", "wrong code not rejected")
		resRight, err := VerifyOtp(phone, code)
		kitExpect(err == nil && resRight.Status == "OK" && resRight.Token != "", fmt.Sprintf("correct code gave %v", resRight))
		token = resRight.Token
	})

	kitTest("a code works only once", func() {
		r, _ := VerifyOtp(phone, code)
		kitExpect(r.Status == "EXPIRED", "the same code worked twice")
	})

	kitTest("session:{token} is a HASH with the phone, TTL 30 min", func() {
		t, _ := RedisStr("TYPE", "session:"+token)
		kitExpect(t == "hash", "session key should be a hash")
		s, err := Session(token)
		kitExpect(err == nil && s != nil && s["phone"] == phone, fmt.Sprintf("Session returned %v", s))
	})

	kitTest("using a session pushes its expiry back (sliding)", func() {
		_, _ = RedisCall("EXPIRE", "session:"+token, 100)
		_, _ = Session(token)
		ttl, _ := RedisNum("TTL", "session:"+token)
		kitExpect(ttl > 1790, fmt.Sprintf("TTL is %d", ttl))
	})

	kitTest("3 wrong guesses -> LOCKED, even for the right code", func() {
		p2 := "91" + runID
		_, _ = RedisCall("DEL", "otp:cooldown:"+p2)
		c, _ := RequestOtp(p2)
		bad := "999999"
		if c == "999999" {
			bad = "888888"
		}
		for i := 0; i < 3; i++ {
			_, _ = VerifyOtp(p2, bad)
		}
		r, _ := VerifyOtp(p2, c)
		kitExpect(r.Status == "LOCKED", "4th attempt with the right code wasn't LOCKED")
	})

	kitTest("logout deletes the session; unknown tokens -> null", func() {
		ok, err := Logout(token)
		kitExpect(err == nil && ok, "Logout returned false")
		s, _ := Session(token)
		kitExpect(s == nil, "session still works after logout")
		ok2, _ := Logout(token)
		kitExpect(!ok2, "second logout should return false")
		s2, _ := Session("not-a-token")
		kitExpect(s2 == nil, "unknown token should give nil")
	})
}

// =====================================================================
func checkTrackC() {
	kitSection("C1 FlashSale (Lua atomicity)")
	saleInfo := func() string {
		sale := "naive-" + runID
		_ = StartSale(sale, 50)
		res := runConcurrently(200, func(i int) string {
			return BuyNaive(sale, fmt.Sprintf("u%d", i))
		})
		sold := 0
		for _, s := range res {
			if s == "OK" {
				sold++
			}
		}
		extra := ""
		if sold > 50 {
			extra = "  <- OVERSOLD"
		}
		return fmt.Sprintf("for comparison, BuyNaive sold %d of 50 plates%s", sold, extra)
	}
	kitInfo(saleInfo())

	kitTest("rules on one goroutine: OK, ALREADY_BOUGHT, OK, SOLD_OUT", func() {
		sale := "s1-" + runID
		_ = StartSale(sale, 2)
		got := []string{Buy(sale, "a"), Buy(sale, "a"), Buy(sale, "b"), Buy(sale, "c")}
		expected := []string{"OK", "ALREADY_BOUGHT", "OK", "SOLD_OUT"}
		kitExpect(fmt.Sprint(got) == fmt.Sprint(expected), fmt.Sprintf("got %v", got))
	})

	kitTest("400 concurrent buyers (250 users), stock 100 -> exactly 100 sold, stock 0", func() {
		sale := "s2-" + runID
		_ = StartSale(sale, 100)
		res := runConcurrently(400, func(i int) string {
			return Buy(sale, fmt.Sprintf("u%d", i%250))
		})
		okCount := 0
		for _, s := range res {
			if s == "OK" {
				okCount++
			}
		}
		kitExpect(okCount == 100, fmt.Sprintf("%d sold", okCount))
		rem := SaleRemaining(sale)
		kitExpect(rem == 0, fmt.Sprintf("stock is %d", rem))
		buyers := SaleBuyerCount(sale)
		kitExpect(buyers == 100, fmt.Sprintf("buyers set has %d", buyers))
	})

	kitTest("one user hammering buy 30x at once -> 1 plate", func() {
		sale := "s3-" + runID
		_ = StartSale(sale, 10)
		res := runConcurrently(30, func(i int) string {
			return Buy(sale, "greedy")
		})
		okCount := 0
		for _, s := range res {
			if s == "OK" {
				okCount++
			}
		}
		kitExpect(okCount == 1, fmt.Sprintf("greedy got %d plates", okCount))
	})

	kitSection("C2 RiderLock (distributed lock)")
	kitTest("tryLock: first caller gets a token, second gets null, key has a TTL", func() {
		n := "t1-" + runID
		a, errA := TryLock(n, 5000)
		kitExpect(errA == nil && a != "", "first TryLock returned empty")
		b, _ := TryLock(n, 5000)
		kitExpect(b == "", "second TryLock also got the lock!")
		pttl, _ := RedisNum("PTTL", "lock:"+n)
		kitExpect(pttl > 0 && pttl <= 5000, fmt.Sprintf("PTTL is %d -- a crashed holder would block forever", pttl))
	})

	kitTest("unlock with the wrong token does nothing; the right token releases", func() {
		n := "t2-" + runID
		a, _ := TryLock(n, 5000)
		unlockedWrong, _ := Unlock(n, "someone-else")
		kitExpect(!unlockedWrong, "unlock with a wrong token returned true")
		exists, _ := RedisNum("EXISTS", "lock:"+n)
		kitExpect(exists == 1, "lock was deleted by a stranger")
		unlockedRight, _ := Unlock(n, a)
		kitExpect(unlockedRight, "unlock with the right token returned false")
		existsAfter, _ := RedisNum("EXISTS", "lock:"+n)
		kitExpect(existsAfter == 0, "lock still exists")
	})

	kitTest("an expired lock can be taken again", func() {
		n := "t3-" + runID
		_, _ = TryLock(n, 200)
		time.Sleep(300 * time.Millisecond)
		second, _ := TryLock(n, 1000)
		kitExpect(second != "", "lock never expired")
	})

	kitTest("slow holder's late unlock must NOT delete the next holder's lock", func() {
		n := "t4-" + runID
		slow, _ := TryLock(n, 150)
		time.Sleep(250 * time.Millisecond) // slow holder's lock expired...
		next, _ := TryLock(n, 5000)        // ...someone else took it
		kitExpect(next != "", "couldn't take the expired lock")
		unlockedSlow, _ := Unlock(n, slow)
		kitExpect(!unlockedSlow, "the slow holder's unlock returned true")
		curLock, _ := RedisStr("GET", "lock:"+n)
		kitExpect(curLock == next, "the new holder lost its lock")
	})

	kitTest("30 orders at once, 5 riders -> 5 assigned, each rider exactly once", func() {
		riders := []string{"ra-" + runID, "rb-" + runID, "rc-" + runID, "rd-" + runID, "re-" + runID}
		for _, r := range riders {
			_ = ReleaseRider(r)
		}
		got := runConcurrently(30, func(i int) string {
			r, _ := AssignRider(fmt.Sprintf("ord-%d", i), riders)
			return r
		})
		assigned := make([]string, 0)
		seen := make(map[string]bool)
		for _, r := range got {
			if r != "" {
				assigned = append(assigned, r)
				seen[r] = true
			}
		}
		kitExpect(len(assigned) == 5, fmt.Sprintf("%d assignments for 5 riders", len(assigned)))
		kitExpect(len(seen) == 5, fmt.Sprintf("a rider got two orders: %v", assigned))
		for _, r := range riders {
			_ = ReleaseRider(r)
		}
	})
}

// =====================================================================
func checkTrackD() {
	kitSection("D1 Leaderboard (SORTED SET)")
	board := "lb:test:" + runID
	kitTest("recordOrder increments and returns the new score", func() {
		for i := 0; i < 3; i++ {
			_, _ = RecordOrder(board, "r1")
		}
		for i := 0; i < 4; i++ {
			_, _ = RecordOrder(board, "r2")
		}
		_, _ = RecordOrder(board, "r3")
		s, err := RecordOrder(board, "r2")
		kitExpect(err == nil && s == 5, fmt.Sprintf("r2 should be at 5, got %d", s))
		t, _ := RedisStr("TYPE", board)
		kitExpect(t == "zset", board+" should be a sorted set")
		ttl, _ := RedisNum("TTL", board)
		kitExpect(ttl > 7*86400, fmt.Sprintf("board needs an ~8 day expiry, TTL is %d", ttl))
	})

	kitTest("top(2) -> Biryani Brothers (5), Dosa Junction (3), ranks from 1", func() {
		top, err := TopLeaderboard(board, 2)
		kitExpect(err == nil && len(top) == 2, fmt.Sprintf("got %d entries", len(top)))
		kitExpect(top[0].Rank == 1 && top[0].RestaurantID == "r2" && top[0].Name == "Biryani Brothers" && top[0].Orders == 5, fmt.Sprintf("first was %+v", top[0]))
		kitExpect(top[1].Rank == 2 && top[1].RestaurantID == "r1" && top[1].Name == "Dosa Junction" && top[1].Orders == 3, fmt.Sprintf("second was %+v", top[1]))
	})

	kitTest("rankOf(r3) -> #3 with 1 order; rankOf(r5) -> null", func() {
		e3, err := RankOf(board, "r3")
		kitExpect(err == nil && e3 != nil && e3.Rank == 3 && e3.Orders == 1, fmt.Sprintf("got %+v", e3))
		e5, err := RankOf(board, "r5")
		kitExpect(err == nil && e5 == nil, "r5 has no orders, expected nil")
	})

	kitTest("empty board -> empty list", func() {
		top, err := TopLeaderboard("lb:empty:"+runID, 10)
		kitExpect(err == nil && len(top) == 0, "expected empty")
	})
	_, _ = RedisCall("DEL", board)

	kitSection("D2 NearbyRiders (GEO)")
	kora := "kora-" + runID
	indira := "indira-" + runID
	white := "white-" + runID

	kitTest("updateLocation stores lng/lat correctly + a 30 s heartbeat", func() {
		_ = UpdateLocation(kora, 12.9352, 77.6245)
		_ = UpdateLocation(indira, 12.9719, 77.6412)
		_ = UpdateLocation(white, 12.9698, 77.7500)

		rawPos, err := RedisList("GEOPOS", "riders:geo", kora)
		kitExpect(err == nil && len(rawPos) > 0 && rawPos[0] != nil, kora+" not in riders:geo")
		coordList := rawPos[0].([]any)
		lng, _ := strconv.ParseFloat(fmt.Sprint(coordList[0]), 64)
		lat, _ := strconv.ParseFloat(fmt.Sprint(coordList[1]), 64)
		kitExpect(math.Abs(lat-12.9352) < 0.001 && math.Abs(lng-77.6245) < 0.001,
			fmt.Sprintf("stored lng=%f lat=%f -- GEOADD takes LONGITUDE first", lng, lat))
		ttl, _ := RedisNum("TTL", "rider:"+kora+":alive")
		kitExpect(ttl > 25 && ttl <= 30, fmt.Sprintf("heartbeat TTL is %d", ttl))
	})

	filterMine := func(all []NearbyRider) []NearbyRider {
		var out []NearbyRider
		for _, n := range all {
			if strings.HasSuffix(n.RiderID, "-"+runID) {
				out = append(out, n)
			}
		}
		return out
	}

	kitTest("nearby(Koramangala, 6 km) -> Koramangala rider then Indiranagar, not Whitefield", func() {
		all, err := Nearby(12.9340, 77.6260, 6, 50)
		kitExpect(err == nil, "Nearby error")
		got := filterMine(all)
		kitExpect(len(got) == 2, fmt.Sprintf("expected 2 test riders, got %+v", got))
		kitExpect(got[0].RiderID == kora && got[1].RiderID == indira, fmt.Sprintf("wrong order: %+v", got))
		kitExpect(got[0].DistanceKm < 0.5, fmt.Sprintf("Koramangala rider should be < 0.5 km, was %f", got[0].DistanceKm))
		kitExpect(got[1].DistanceKm > 3 && got[1].DistanceKm < 6, fmt.Sprintf("Indiranagar should be ~4.5 km, was %f", got[1].DistanceKm))
	})

	kitTest("rider with no heartbeat is skipped AND removed from riders:geo", func() {
		_, _ = RedisCall("DEL", "rider:"+kora+":alive")
		all, _ := Nearby(12.9340, 77.6260, 6, 50)
		got := filterMine(all)
		for _, n := range got {
			kitExpect(n.RiderID != kora, "dead rider still returned")
		}
		score, _ := RedisStr("ZSCORE", "riders:geo", kora)
		kitExpect(score == "", "dead rider still in riders:geo")
	})

	kitTest("goOffline removes the rider", func() {
		_ = GoOffline(indira)
		score, _ := RedisStr("ZSCORE", "riders:geo", indira)
		kitExpect(score == "", "still in riders:geo")
		exists, _ := RedisNum("EXISTS", "rider:"+indira+":alive")
		kitExpect(exists == 0, "alive key still there")
	})

	_, _ = RedisCall("ZREM", "riders:geo", kora, indira, white)
}

// =====================================================================
func checkTrackE() {
	kitSection("E1 LiveUpdates (Pub/Sub)")
	kitTest("publish with nobody listening -> 0 receivers (the message is gone)", func() {
		rec, err := PublishLive("nobody-"+runID, "PREPARING")
		kitExpect(err == nil && rec == 0, fmt.Sprintf("expected 0, got %d", rec))
	})

	kitTest("listen() receives ACCEPTED, PREPARING, DELIVERED in order, then returns", func() {
		order := "live-" + runID
		var mu sync.Mutex
		var got []string

		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			_ = ListenLive(order, &testLiveListener{
				onMsg: func(jsonStr string) bool {
					mu.Lock()
					got = append(got, jsonStr)
					mu.Unlock()
					return true
				},
				onIdle: func() bool { return true },
			})
		}()

		time.Sleep(300 * time.Millisecond)
		receivers, err := PublishLive(order, "ACCEPTED")
		kitExpect(err == nil, "publish error")
		_, _ = PublishLive(order, "PREPARING")
		_, _ = PublishLive(order, "DELIVERED")

		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			panic("ListenLive didn't return after DELIVERED")
		}

		kitExpect(receivers == 1, fmt.Sprintf("publish should reach 1 subscriber, reached %d", receivers))
		mu.Lock()
		defer mu.Unlock()
		kitExpect(len(got) == 3, fmt.Sprintf("received %d messages: %v", len(got), got))
		kitExpect(strings.Contains(got[0], "ACCEPTED") && strings.Contains(got[2], "DELIVERED"), fmt.Sprintf("wrong order: %v", got))
		kitExpect(strings.Contains(got[0], `"orderId":"`+order+`"`), "message should include orderId: "+got[0])
	})

	kitTest("listen() stops when the listener returns false (browser left)", func() {
		order := "live2-" + runID
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			_ = ListenLive(order, &testLiveListener{
				onMsg:  func(s string) bool { return false },
				onIdle: func() bool { return false },
			})
		}()

		time.Sleep(300 * time.Millisecond)
		_, _ = PublishLive(order, "ACCEPTED")

		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			panic("still listening")
		}
	})

	kitSection("E2 OrderPipeline (STREAM + consumer group)")
	p := &OrderPipeline{Stream: "test:orders:" + runID, Group: "kitchen"}
	_ = p.EnsureGroup()

	kitTest("add() appends 10 entries and returns ids", func() {
		reID := regexp.MustCompile(`^\d+-\d+$`)
		for i := 0; i < 10; i++ {
			id, err := p.Add(map[string]string{"orderId": fmt.Sprintf("o%d", i), "restaurantId": "r1"})
			kitExpect(err == nil && reID.MatchString(id), "id looks wrong: "+id)
		}
		xlen, _ := RedisNum("XLEN", p.Stream)
		kitExpect(xlen == 10, fmt.Sprintf("XLEN is %d", xlen))
	})

	kitTest("two workers in one group split the 10 entries -- nobody gets the same one", func() {
		a, errA := p.ReadNew("w1", 6, 100)
		b, errB := p.ReadNew("w2", 10, 100)
		kitExpect(errA == nil && errB == nil, "ReadNew error")
		ids := make(map[string]bool)
		for _, e := range a {
			ids[e.ID] = true
		}
		for _, e := range b {
			ids[e.ID] = true
		}
		kitExpect(len(a) == 6 && len(b) == 4, fmt.Sprintf("w1 got %d, w2 got %d", len(a), len(b)))
		kitExpect(len(ids) == 10, "duplicate deliveries")
		kitExpect(a[0].Fields["orderId"] == "o0", fmt.Sprintf("fields not parsed: %+v", a[0]))

		pending, _ := p.PendingCount()
		kitExpect(pending == 10, fmt.Sprintf("10 should be pending until acked, XPENDING says %d", pending))

		for _, e := range a {
			_, _ = p.Ack(e.ID)
		}
		for _, e := range b {
			acked, _ := p.Ack(e.ID)
			kitExpect(acked == 1, "ack returned 0")
		}
		pendingAfter, _ := p.PendingCount()
		kitExpect(pendingAfter == 0, fmt.Sprintf("pending after acks: %d", pendingAfter))
	})

	kitTest("readNew with nothing new waits ~blockMs and returns an empty list", func() {
		start := time.Now()
		none, err := p.ReadNew("w1", 5, 300)
		kitExpect(err == nil && len(none) == 0, fmt.Sprintf("got %v", none))
		kitExpect(time.Since(start).Milliseconds() >= 250, "didn't block")
	})

	kitTest("crashed worker: its unacked entries are rescued with claimStuck", func() {
		for i := 0; i < 3; i++ {
			_, _ = p.Add(map[string]string{"orderId": fmt.Sprintf("crash%d", i)})
		}
		taken, _ := p.ReadNew("ghost", 10, 100)
		kitExpect(len(taken) == 3, fmt.Sprintf("ghost read %d", len(taken)))
		stuckEarly, _ := p.ClaimStuck("rescuer", 500, 10)
		kitExpect(len(stuckEarly) == 0, "claimed entries that weren't idle long enough")

		time.Sleep(600 * time.Millisecond)
		rescued, err := p.ClaimStuck("rescuer", 500, 10)
		kitExpect(err == nil && len(rescued) == 3, fmt.Sprintf("rescued %d", len(rescued)))
		kitExpect(strings.HasPrefix(rescued[0].Fields["orderId"], "crash"), fmt.Sprintf("wrong entries: %+v", rescued))

		for _, e := range rescued {
			_, _ = p.Ack(e.ID)
		}
		pending, _ := p.PendingCount()
		kitExpect(pending == 0, fmt.Sprintf("still pending: %d", pending))
	})
	_, _ = RedisCall("DEL", p.Stream)

	kitSection("I1 Checkout (integration: needs Tracks A, B, E)")
	phone := "93" + runID
	var checkoutToken string

	kitTest("no / bad token -> 401", func() {
		kitExpect(Checkout("").Status == 401, "empty token not refused")
		kitExpect(Checkout("nope").Status == 401, "bad token not refused")
	})

	kitTest("logged in, empty cart -> 400", func() {
		_, _ = RedisCall("DEL", "otp:cooldown:"+phone, "rl:sliding:"+phone)
		c, _ := RequestOtp(phone)
		v, _ := VerifyOtp(phone, c)
		checkoutToken = v.Token
		_ = CartClear(phone)
		kitExpect(Checkout(checkoutToken).Status == 400, "empty cart not refused")
	})

	kitTest("items from two restaurants -> 400", func() {
		_, _ = CartAddItem(phone, "masala-dosa", 1)
		_, _ = CartAddItem(phone, "veg-biryani", 1)
		kitExpect(Checkout(checkoutToken).Status == 400, "mixed cart not refused")
		_ = CartClear(phone)
	})

	kitTest("2 dosas -> 202, total 258, order on orders:stream, cart emptied", func() {
		_, _ = CartAddItem(phone, "masala-dosa", 2)
		r := Checkout(checkoutToken)
		kitExpect(r.Status == 202, fmt.Sprintf("status %d %v", r.Status, r.Body))
		orderID, _ := r.Body["orderId"].(string)
		totalVal := fmt.Sprint(r.Body["total"])
		kitExpect(orderID != "" && totalVal == "258", fmt.Sprintf("body: %v", r.Body))

		items, _ := CartItems(phone)
		kitExpect(len(items) == 0, "cart not cleared")

		recent, _ := RedisList("XREVRANGE", "orders:stream", "+", "-", "COUNT", 50)
		kitExpect(strings.Contains(fmt.Sprint(recent), orderID), orderID+" not found on orders:stream")
	})

	kitTest("6th checkout within a minute -> 429 with retryAfterSeconds", func() {
		var lastResp CheckoutResponse
		for i := 0; i < 3; i++ {
			_, _ = CartAddItem(phone, "filter-coffee", 1)
			lastResp = Checkout(checkoutToken)
		}
		kitExpect(lastResp.Status == 429, fmt.Sprintf("expected 429 on the 6th call, got %d", lastResp.Status))
		_, hasRetry := lastResp.Body["retryAfterSeconds"]
		kitExpect(hasRetry, "429 body needs retryAfterSeconds")
		_ = CartClear(phone)
	})

	kitSection("I2 KitchenWorker.process (integration: needs every track)")
	kitTest("process() publishes ACCEPTED..DELIVERED, bumps the leaderboard, frees the rider", func() {
		KitchenStepMs = 50 * time.Millisecond
		for id, start := range RiderStarts {
			_ = UpdateLocation(id, start[0], start[1])
			_ = ReleaseRider(id)
		}

		order := "kw-" + runID
		board := LeaderboardThisWeek()
		before, _ := RankOf(board, "r1")

		var mu sync.Mutex
		var got []string
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			_ = ListenLive(order, &testLiveListener{
				onMsg: func(jsonStr string) bool {
					status := extractField(jsonStr, "status")
					mu.Lock()
					got = append(got, status)
					mu.Unlock()
					return true
				},
				onIdle: func() bool { return true },
			})
		}()

		time.Sleep(300 * time.Millisecond)
		err := ProcessOrder(StreamEntry{
			ID: "0-1",
			Fields: map[string]string{
				"orderId":      order,
				"restaurantId": "r1",
			},
		})
		kitExpect(err == nil, fmt.Sprintf("ProcessOrder error: %v", err))

		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			panic("ListenLive didn't finish")
		}

		mu.Lock()
		expectedStatuses := []string{"ACCEPTED", "PREPARING", "OUT_FOR_DELIVERY", "DELIVERED"}
		kitExpect(fmt.Sprint(got) == fmt.Sprint(expectedStatuses), fmt.Sprintf("statuses: %v", got))
		mu.Unlock()

		after, _ := RankOf(board, "r1")
		beforeOrders := int64(0)
		if before != nil {
			beforeOrders = before.Orders
		}
		kitExpect(after != nil && after.Orders == beforeOrders+1, "leaderboard not incremented")

		for r := range RiderStarts {
			heldOrder, _ := RedisStr("GET", "rider:"+r+":order")
			kitExpect(heldOrder != order, r+" still holds the delivered order")
		}
	})
}

type testLiveListener struct {
	onMsg  func(string) bool
	onIdle func() bool
}

func (l *testLiveListener) OnMessage(jsonStr string) bool { return l.onMsg(jsonStr) }
func (l *testLiveListener) OnIdle() bool                 { return l.onIdle() }

func extractField(jsonStr, fieldName string) string {
	re := regexp.MustCompile(`"` + fieldName + `"\s*:\s*"([^"]*)"`)
	m := re.FindStringSubmatch(jsonStr)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
