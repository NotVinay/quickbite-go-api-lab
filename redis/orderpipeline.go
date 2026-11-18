package main

import (
	"fmt"
	"strings"
)

// TRACK E · Feature E2 — Reliable order queue with Streams.      Redis type: STREAM + consumer group
//
// Problem: checkout must answer the user in milliseconds, but "send to restaurant, assign rider,
// update leaderboard..." takes seconds. So checkout just appends the order to a queue, and kitchen
// workers process it in the background. Pub/Sub can't do this: if no worker is listening, the order
// is lost. A STREAM is an append-only log that remembers every entry; a CONSUMER GROUP gives each
// entry to exactly one worker and tracks which ones were never acknowledged (the worker crashed).
//
// Keys you own:   orders:stream   STREAM   one entry per order   (group "kitchen")
//
// Try it:  redis-cli XLEN orders:stream    redis-cli XRANGE orders:stream - +    redis-cli XPENDING orders:stream kitchen

// StreamEntry represents one stream entry: its id (e.g. "1731234567890-0") and its fields.
type StreamEntry struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
}

type OrderPipeline struct {
	Stream string
	Group  string
}

var MainOrderPipeline = &OrderPipeline{
	Stream: "orders:stream",
	Group:  "kitchen",
}

// GIVEN: create the consumer group (and the stream) once. "0" = the group starts from the first entry.
func (p *OrderPipeline) EnsureGroup() error {
	_, err := RedisCall("XGROUP", "CREATE", p.Stream, p.Group, "0", "MKSTREAM")
	if err != nil {
		if strings.Contains(err.Error(), "BUSYGROUP") {
			return nil // already exists: fine
		}
		return err
	}
	return nil
}

// TODO(1): Append an order.
//   XADD orders:stream MAXLEN ~ 100000 * field1 value1 field2 value2 ...
//     "*"          = let Redis generate the id (timestamp-sequence)
//     "MAXLEN ~ N" = keep roughly the last N entries so memory can't grow forever
//   Return the generated id string.
func (p *OrderPipeline) Add(fields map[string]string) (string, error) {
	panic("TODO(1) not implemented yet")
}

// TODO(2): Read entries no other worker in the group has received yet.
//   XREADGROUP GROUP {group} {consumer} COUNT {count} BLOCK {blockMs} STREAMS {stream} >
//     ">" = "give me new entries". Nothing new within blockMs -> reply is nil/empty -> return empty slice.
//   The reply shape is nested — ParseRead(reply) below turns it into []StreamEntry.
func (p *OrderPipeline) ReadNew(consumer string, count int, blockMs int64) ([]StreamEntry, error) {
	panic("TODO(2) not implemented yet")
}

// TODO(3): Acknowledge = "done, don't give this to anyone again". XACK {stream} {group} {id}. Return count acked (int64).
func (p *OrderPipeline) Ack(id string) (int64, error) {
	panic("TODO(3) not implemented yet")
}

// TODO(4): Crash recovery. A worker read an entry and died before XACK. It sits in the
//   "pending" list forever unless someone takes it over.
//   XAUTOCLAIM {stream} {group} {consumer} {minIdleMs} 0-0 COUNT {count}
//     -> moves entries idle for >= minIdleMs to `consumer` and returns them.
//     Reply: [nextCursor, [[id, [f, v, ...]], ...], [deletedIds]] -> ParseEntries(reply[1])
func (p *OrderPipeline) ClaimStuck(consumer string, minIdleMs int64, count int) ([]StreamEntry, error) {
	panic("TODO(4) not implemented yet")
}

// GIVEN: how many entries were delivered to some worker but never acked.
func (p *OrderPipeline) PendingCount() (int64, error) {
	summary, err := RedisList("XPENDING", p.Stream, p.Group)
	if err != nil {
		return 0, err
	}
	if len(summary) == 0 || summary[0] == nil {
		return 0, nil
	}
	switch v := summary[0].(type) {
	case int64:
		return v, nil
	default:
		var n int64
		fmt.Sscan(fmt.Sprint(v), &n)
		return n, nil
	}
}

// ---------------- GIVEN: reply parsing ----------------

// ParseRead parses an XREADGROUP reply: [[streamName, [[id, [f, v, ...]], ...]]] or nil.
func ParseRead(reply any) []StreamEntry {
	if reply == nil {
		return nil
	}
	streams, ok := reply.([]any)
	if !ok || len(streams) == 0 {
		return nil
	}
	first, ok := streams[0].([]any)
	if !ok || len(first) < 2 {
		return nil
	}
	entriesRaw, ok := first[1].([]any)
	if !ok {
		return nil
	}
	return ParseEntries(entriesRaw)
}

func ParseEntries(raw []any) []StreamEntry {
	var out []StreamEntry
	for _, o := range raw {
		e, ok := o.([]any)
		if !ok || len(e) < 2 {
			continue
		}
		id := fmt.Sprint(e[0])
		kv, ok := e[1].([]any)
		if !ok {
			continue // entry was trimmed away
		}
		fields := make(map[string]string)
		for i := 0; i+1 < len(kv); i += 2 {
			fields[fmt.Sprint(kv[i])] = fmt.Sprint(kv[i+1])
		}
		out = append(out, StreamEntry{ID: id, Fields: fields})
	}
	return out
}
