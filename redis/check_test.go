package main

import (
	"testing"
)

func TestRedisConnection(t *testing.T) {
	pong, err := RedisStr("PING")
	if err != nil {
		t.Skipf("Skipping Redis tests: Redis not reachable at %s:%s (%v)", getRedisHost(), getRedisPort(), err)
	}
	if pong != "PONG" {
		t.Fatalf("Expected PONG, got %s", pong)
	}
}
