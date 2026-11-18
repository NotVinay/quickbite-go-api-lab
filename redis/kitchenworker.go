package main

import (
	"fmt"
	"os"
	"time"
)

// TRACK E · Integration I2 — the kitchen worker. A separate process that drains orders:stream.
//
//   ./run.sh worker w1              start a worker named w1 (start w2 in another terminal too!)
//   ./run.sh worker w3 --crash      reads ONE order and dies without XACK -> watch w1/w2 rescue it
//
//   orders:stream ──XREADGROUP──► worker ──► ACCEPTED ──► PREPARING ──► rider assigned ──► OUT_FOR_DELIVERY ──► DELIVERED ──► XACK
//                                              │            │                 │                                   │
//                                         publish (E1)  leaderboard (D1)  nearby (D2) + lock (C2)          release rider (C2)

// KitchenStepMs is how long each kitchen step "takes". The checker shortens it.
var KitchenStepMs = 2000 * time.Millisecond

// RunKitchenWorker runs the kitchen worker main loop.
func RunKitchenWorker(name string, crash bool) {
	pipeline := MainOrderPipeline
	if err := pipeline.EnsureGroup(); err != nil {
		fmt.Printf("Warning ensuring stream group: %v\n", err)
	}

	crashNotice := ""
	if crash {
		crashNotice = " [will CRASH after reading one order]"
	}
	fmt.Printf("Kitchen worker %s started%s\n", name, crashNotice)

	for {
		// First rescue orders another worker took but never finished (idle > 15 s)...
		batch, err := pipeline.ClaimStuck(name, 15000, 10)
		if err == nil && len(batch) > 0 {
			fmt.Printf("[%s] rescued %d stuck order(s) from a dead worker\n", name, len(batch))
		}

		// ...then wait up to 5 s for new ones.
		if len(batch) == 0 {
			batch, _ = pipeline.ReadNew(name, 5, 5000)
		}

		for _, e := range batch {
			if crash {
				fmt.Printf("[%s] took %s ... and CRASHED (no XACK)\n", name, e.Fields["orderId"])
				os.Exit(1)
			}
			if err := ProcessOrder(e); err != nil {
				// No ack: the entry stays pending and will be claimed again after 15 s.
				fmt.Printf("[%s] failed %s: %v\n", name, e.ID, err)
			} else {
				_, _ = pipeline.Ack(e.ID)
			}
		}
	}
}

// TODO(1): Cook and deliver one order, using everyone's features.
//   orderId = e.Fields["orderId"], restaurantId = e.Fields["restaurantId"]
//   1. PublishLive(orderId, "ACCEPTED")
//      RecordOrder(LeaderboardThisWeek(), restaurantId)
//   2. time.Sleep(KitchenStepMs);  PublishLive(orderId, "PREPARING");  time.Sleep(KitchenStepMs)
//   3. Find riders: loc = RestaurantLocations[restaurantId]
//      Nearby(loc[0], loc[1], 10, 5) -> rider ids, nearest first
//      rider = AssignRider(orderId, ids)
//      no rider -> return fmt.Errorf("no free rider")   (no ack -> retried later)
//   4. PublishLive(orderId, "OUT_FOR_DELIVERY");  time.Sleep(KitchenStepMs)
//   5. PublishLive(orderId, "DELIVERED");  ReleaseRider(rider)
//   Print a line for each step so the demo is easy to follow.
func ProcessOrder(e StreamEntry) error {
	panic("TODO(1) not implemented yet")
}

func LogKitchen(orderID, msg string) {
	fmt.Printf("  [kitchen] %s -> %s\n", orderID, msg)
}
