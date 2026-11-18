package main

import (
	"strconv"
	"time"
)

// TRACK C · Feature C1 — Flash sale: "₹1 Biryani, 100 plates, 8 PM sharp".   Redis: counter + SET + Lua
//
// Problem: at 8:00:00 PM, 20,000 users hit "Buy" within one second. Rules:
//   - never sell more than the stock (overselling = refunds + angry restaurant)
//   - one plate per user
// Reading the stock, checking it, and writing it back is three steps. Between them, other requests
// sneak in. BuyNaive() below shows the bug — the checker proves it oversells.
//
// Keys you own:   sale:{saleId}:stock    STRING  remaining plates
//                 sale:{saleId}:buyers   SET     userIds who bought
//
//   POST /sales/{saleId}/start {"stock":100}     POST /sales/{saleId}/buy {"userId":"u1"}     GET /sales/{saleId}

func SaleStockKey(saleID string) string {
	return "sale:" + saleID + ":stock"
}

func SaleBuyersKey(saleID string) string {
	return "sale:" + saleID + ":buyers"
}

// GIVEN: (re)start a sale.
func StartSale(saleID string, stock int) error {
	if _, err := RedisCall("SET", SaleStockKey(saleID), stock); err != nil {
		return err
	}
	_, err := RedisCall("DEL", SaleBuyersKey(saleID))
	return err
}

// GIVEN — and BROKEN. Read-check-write in Go: correct on one goroutine, oversells under load.
func BuyNaive(saleID, userID string) string {
	isMember, _ := RedisNum("SISMEMBER", SaleBuyersKey(saleID), userID)
	if isMember == 1 {
		return "ALREADY_BOUGHT"
	}
	stockStr, _ := RedisStr("GET", SaleStockKey(saleID))
	stock, _ := strconv.ParseInt(stockStr, 10, 64)
	if stock <= 0 {
		return "SOLD_OUT"
	}
	time.Sleep(1 * time.Millisecond) // payment pre-check, logging... any small delay
	_, _ = RedisCall("SET", SaleStockKey(saleID), stock-1) // <- another goroutine read the same stock!
	_, _ = RedisCall("SADD", SaleBuyersKey(saleID), userID)
	return "OK"
}

// TODO(1): The same logic, but as ONE Lua script. Redis runs a script start-to-finish with nothing
// else in between — so check-then-act becomes atomic across all your servers.
//   KEYS[1] = stock key, KEYS[2] = buyers key, ARGV[1] = userId
//   - user already in the buyers SET (SISMEMBER == 1)  -> return 'ALREADY_BOUGHT'
//   - stock (GET, tonumber(...)) <= 0                    -> return 'SOLD_OUT'
//   - otherwise DECR stock, SADD the user                -> return 'OK'
const BuyLua = `
-- write your script here
`

// TODO(2): Run BuyLua with RedisEval(script, keys, args...) and return its reply ("OK", "SOLD_OUT"...).
func Buy(saleID, userID string) string {
	panic("TODO(2) not implemented yet")
}

// GIVEN: current state for the API / checker.
func SaleRemaining(saleID string) int64 {
	s, err := RedisStr("GET", SaleStockKey(saleID))
	if err != nil || s == "" {
		return 0
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func SaleBuyerCount(saleID string) int64 {
	n, _ := RedisNum("SCARD", SaleBuyersKey(saleID))
	return n
}
