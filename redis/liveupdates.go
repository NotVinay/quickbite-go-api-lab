package main

// TRACK E · Feature E1 — Live order status with Pub/Sub.       Redis: PUBLISH / SUBSCRIBE
//
// Problem: the kitchen worker (one process) moves an order to PREPARING. The customer's phone is
// connected via SSE to API server #7 (another process). How does server #7 find out?
// Pub/Sub: the worker PUBLISHes to a channel, every server SUBSCRIBEd to it gets the message instantly.
// (This connects to Exercise 03's SSE, now working across many servers.)
//
// Channels you own:   order:{orderId}:status       (channels are not keys — they store nothing!)
//
//   GET /orders/{orderId}/live    -> text/event-stream, one event per status change
//
// Try it:  terminal 1: redis-cli SUBSCRIBE order:ord_1:status     terminal 2: redis-cli PUBLISH order:ord_1:status hi

func LiveChannel(orderID string) string {
	return "order:" + orderID + ":status"
}

// LiveListener is called by the SSE endpoint. Return false from a method to stop listening (the browser left).
type LiveListener interface {
	OnMessage(jsonStr string) bool
	OnIdle() bool
}

// TODO(1): Announce a status change.
//   jsonStr = marshal map of {"orderId": orderID, "status": status, "at": time.Now().UnixMilli()}
//   PUBLISH order:{orderId}:status jsonStr    -> returns how many subscribers received it. Return that.
//   Note: if nobody is subscribed right now, the message is simply lost. (docs/TRACK-E.md Q1)
func PublishLive(orderID, status string) (int64, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Listen for one order's updates until it's DELIVERED or the listener says stop.
//   sub, err := RedisSubscribe(LiveChannel(orderID))
//   defer sub.Close()
//   loop:
//     msg, err := sub.Next(15 * time.Second)      // waits up to 15 s; empty string = nothing came
//     empty   -> if !listener.OnIdle() return nil;          (SSE sends a keep-alive here)
//     message -> if !listener.OnMessage(msg) return nil;
//                if strings.Contains(msg, `"DELIVERED"`) return nil; (we're done)
func ListenLive(orderID string, listener LiveListener) error {
	panic("TODO(2) not implemented yet")
}
