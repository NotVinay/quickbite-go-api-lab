package main

import (
	"fmt"
	"time"
)

// TRACK D · Feature D1 — "Top restaurants this week" leaderboard.     Redis type: SORTED SET
//
// Problem: the home screen shows the 10 most-ordered restaurants this week, and each restaurant's
// dashboard shows "You're #7 in Bengaluru". Doing ORDER BY COUNT(*) over millions of orders on every
// app open is slow. A sorted set keeps members ordered by score at all times: +1 is O(log N),
// top-10 is O(log N + 10).
//
// Keys you own:   lb:restaurants:{year}-W{week}    ZSET  member = restaurantId, score = orders
//
//   GET /leaderboard?top=10      GET /leaderboard/{restaurantId}
//
// Try it:  redis-cli ZREVRANGE lb:restaurants:2026-W47 0 -1 WITHSCORES

const LeaderboardKeepDays = 8

// LeaderboardEntry holds a restaurant's rank and orders. Rank is 1-based (1 = most orders).
type LeaderboardEntry struct {
	Rank         int64  `json:"rank"`
	RestaurantID string `json:"restaurantId"`
	Name         string `json:"name"`
	Orders       int64  `json:"orders"`
}

// GIVEN: this week's board, e.g. lb:restaurants:2026-W47. A new key every week = automatic weekly reset.
func LeaderboardThisWeek() string {
	year, week := time.Now().ISOWeek()
	return fmt.Sprintf("lb:restaurants:%d-W%02d", year, week)
}

// TODO(1): An order was placed at restaurantId.
//   ZINCRBY board 1 restaurantId     (creates the member at 0 if missing)
//   EXPIRE board (LeaderboardKeepDays days in seconds = 8 * 86400) so old weeks clean themselves up.
//   Return the new score.
func RecordOrder(board, restaurantID string) (int64, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Top n, highest first.
//   ZREVRANGE board 0 (n-1) WITHSCORES  ->  [member1, score1, member2, score2, ...]   (scores are strings like "42")
//   Build LeaderboardEntry{Rank: rank, RestaurantID: id, Name: FakeDbRestaurantName(id), Orders: orders} with rank starting at 1.
func TopLeaderboard(board string, n int) ([]LeaderboardEntry, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): One restaurant's position. ZREVRANK gives the 0-based rank (nil if not on the board),
//   ZSCORE gives the score. Return nil if the restaurant has no orders this week.
//   Ties: two restaurants with equal scores are ordered by member name — fine for us.
func RankOf(board, restaurantID string) (*LeaderboardEntry, error) {
	panic("TODO(3) not implemented yet")
}
