package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GIVEN — a tiny Redis client (about 200 lines) using only the Go standard library.
//
// You send the SAME words you would type in redis-cli:
//
//     RedisCall("SET", "menu:r1", jsonStr, "EX", 60)          // -> "OK"
//     v, _ := RedisStr("GET", "menu:r1")                       // -> the value string, or ""
//     n, _ := RedisNum("INCR", "rl:user42")                    // -> int64 number
//     l, _ := RedisList("ZREVRANGE", "lb", 0, 9, "WITHSCORES")
//
// Replies come back as: string, int64, []any, or nil.
// Redis errors (e.g. "WRONGTYPE ...") are returned as standard Go errors.
//
// Connection settings come from environment variables:
//   REDIS_HOST (localhost), REDIS_PORT (6379), REDIS_PASSWORD.

const (
	maxConnections = 64
)

var (
	pool        = make(chan *redisConn, maxConnections)
	createdConn int32
	poolMu      sync.Mutex
)

type redisConn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func getRedisHost() string {
	if h := os.Getenv("REDIS_HOST"); h != "" {
		return h
	}
	return "localhost"
}

func getRedisPort() string {
	if p := os.Getenv("REDIS_PORT"); p != "" {
		return p
	}
	return "6379"
}

func openRedisConn() (*redisConn, error) {
	addr := net.JoinHostPort(getRedisHost(), getRedisPort())
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Redis at %s: %w (start it with: docker compose up -d)", addr, err)
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}

	rc := &redisConn{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}

	if pass := os.Getenv("REDIS_PASSWORD"); pass != "" {
		if _, err := rc.send("AUTH", pass); err != nil {
			conn.Close()
			return nil, fmt.Errorf("redis auth failed: %w", err)
		}
	}

	return rc, nil
}

func borrowConn() (*redisConn, error) {
	for {
		select {
		case conn := <-pool:
			return conn, nil
		default:
			poolMu.Lock()
			if atomic.LoadInt32(&createdConn) < maxConnections {
				atomic.AddInt32(&createdConn, 1)
				poolMu.Unlock()
				conn, err := openRedisConn()
				if err != nil {
					atomic.AddInt32(&createdConn, -1)
					return nil, err
				}
				return conn, nil
			}
			poolMu.Unlock()

			// All connections busy, wait for one to return
			conn := <-pool
			return conn, nil
		}
	}
}

func releaseConn(conn *redisConn, healthy bool) {
	if healthy {
		pool <- conn
	} else {
		if conn != nil && conn.conn != nil {
			_ = conn.conn.Close()
		}
		atomic.AddInt32(&createdConn, -1)
	}
}

func (rc *redisConn) writeArgs(args ...any) error {
	if err := rc.w.WriteByte('*'); err != nil {
		return err
	}
	if _, err := rc.w.WriteString(strconv.Itoa(len(args))); err != nil {
		return err
	}
	if _, err := rc.w.WriteString("\r\n"); err != nil {
		return err
	}

	for _, a := range args {
		s := fmt.Sprint(a)
		if err := rc.w.WriteByte('$'); err != nil {
			return err
		}
		if _, err := rc.w.WriteString(strconv.Itoa(len(s))); err != nil {
			return err
		}
		if _, err := rc.w.WriteString("\r\n"); err != nil {
			return err
		}
		if _, err := rc.w.WriteString(s); err != nil {
			return err
		}
		if _, err := rc.w.WriteString("\r\n"); err != nil {
			return err
		}
	}
	return rc.w.Flush()
}

func (rc *redisConn) readReply() (any, error) {
	prefix, err := rc.r.ReadByte()
	if err != nil {
		return nil, err
	}
	return rc.parseReply(prefix)
}

func (rc *redisConn) parseReply(prefix byte) (any, error) {
	line, err := rc.readLine()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '+': // Simple string
		return line, nil
	case '-': // Error
		return nil, errors.New(line)
	case ':': // Integer
		return strconv.ParseInt(line, 10, 64)
	case '$': // Bulk string
		lenVal, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if lenVal < 0 {
			return nil, nil // Nil bulk string
		}
		buf := make([]byte, lenVal+2) // data + \r\n
		if _, err := io.ReadFull(rc.r, buf); err != nil {
			return nil, err
		}
		return string(buf[:lenVal]), nil
	case '*': // Array
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, nil // Nil array
		}
		items := make([]any, count)
		for i := 0; i < count; i++ {
			item, err := rc.readReply()
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unexpected reply prefix: %c", prefix)
	}
}

func (rc *redisConn) readLine() (string, error) {
	var sb strings.Builder
	for {
		b, err := rc.r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\r' {
			next, err := rc.r.ReadByte()
			if err != nil {
				return "", err
			}
			if next != '\n' {
				return "", fmt.Errorf("expected \\n after \\r, got %c", next)
			}
			return sb.String(), nil
		}
		sb.WriteByte(b)
	}
}

func (rc *redisConn) send(args ...any) (any, error) {
	if err := rc.writeArgs(args...); err != nil {
		return nil, err
	}
	return rc.readReply()
}

// RedisCall executes a single command on a pooled Redis connection.
func RedisCall(args ...any) (any, error) {
	conn, err := borrowConn()
	if err != nil {
		return nil, err
	}
	reply, err := conn.send(args...)
	releaseConn(conn, err == nil || isAppError(err))
	return reply, err
}

func isAppError(err error) bool {
	// If it's a Redis-level error like WRONGTYPE, connection is still healthy
	return err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "connection")
}

// RedisStr returns the reply converted to a string. Returns "" if nil.
func RedisStr(args ...any) (string, error) {
	res, err := RedisCall(args...)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return fmt.Sprint(res), nil
}

// RedisNum returns the reply as int64. Returns 0 if nil.
func RedisNum(args ...any) (int64, error) {
	res, err := RedisCall(args...)
	if err != nil {
		return 0, err
	}
	if res == nil {
		return 0, nil
	}
	switch v := res.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return strconv.ParseInt(fmt.Sprint(v), 10, 64)
	}
}

// RedisList returns the reply as a slice of any.
func RedisList(args ...any) ([]any, error) {
	res, err := RedisCall(args...)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	if list, ok := res.([]any); ok {
		return list, nil
	}
	return nil, fmt.Errorf("expected array, got %T", res)
}

// RedisEval executes an EVAL script: RedisEval(script, []string{key1, key2}, arg1, arg2...)
func RedisEval(luaScript string, keys []string, args ...any) (any, error) {
	allArgs := make([]any, 0, 3+len(keys)+len(args))
	allArgs = append(allArgs, "EVAL", luaScript, len(keys))
	for _, k := range keys {
		allArgs = append(allArgs, k)
	}
	allArgs = append(allArgs, args...)
	return RedisCall(allArgs...)
}

// Subscription represents a dedicated Pub/Sub connection.
type Subscription struct {
	conn *redisConn
}

// RedisSubscribe opens a dedicated connection and subscribes to channels.
func RedisSubscribe(channels ...string) (*Subscription, error) {
	conn, err := openRedisConn()
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(channels)+1)
	args = append(args, "SUBSCRIBE")
	for _, ch := range channels {
		args = append(args, ch)
	}
	if err := conn.writeArgs(args...); err != nil {
		_ = conn.conn.Close()
		return nil, err
	}
	// Read confirmation for each channel
	for range channels {
		if _, err := conn.readReply(); err != nil {
			_ = conn.conn.Close()
			return nil, err
		}
	}
	return &Subscription{conn: conn}, nil
}

// Next waits up to timeout for the next message on subscribed channels.
// Returns message payload, or "" if timeout reached without message.
func (s *Subscription) Next(timeout time.Duration) (string, error) {
	if timeout > 0 {
		_ = s.conn.conn.SetReadDeadline(time.Now().Add(timeout))
	} else {
		_ = s.conn.conn.SetReadDeadline(time.Time{})
	}

	reply, err := s.conn.readReply()
	_ = s.conn.conn.SetReadDeadline(time.Time{}) // clear deadline

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return "", nil // timed out, no message
		}
		return "", err
	}

	if items, ok := reply.([]any); ok && len(items) >= 3 {
		// ["message", channel, payload]
		if fmt.Sprint(items[0]) == "message" {
			return fmt.Sprint(items[2]), nil
		}
		// If other event, read next
		return s.Next(timeout)
	}
	return "", nil
}

// Close closes the subscription connection.
func (s *Subscription) Close() error {
	if s.conn != nil && s.conn.conn != nil {
		return s.conn.conn.Close()
	}
	return nil
}
