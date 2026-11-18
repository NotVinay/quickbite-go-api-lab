package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
)

func main() {
	user := "Rider_Ravi"
	if len(os.Args) > 1 {
		user = os.Args[1]
	}
	order := "ord_42"
	if len(os.Args) > 2 {
		order = os.Args[2]
	}

	conn, err := net.Dial("tcp", "localhost:8084")
	if err != nil {
		log.Fatalf("Cannot connect to localhost:8084. Start server first: go run ./go/04-websockets/server/main.go")
	}
	defer conn.Close()

	// 1. Send HTTP Upgrade handshake
	rawNonce := make([]byte, 16)
	_, _ = rand.Read(rawNonce)
	secKey := base64.StdEncoding.EncodeToString(rawNonce)

	req := fmt.Sprintf("GET /chat?order=%s&user=%s HTTP/1.1\r\n"+
		"Host: localhost:8084\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", url.QueryEscape(order), url.QueryEscape(user), secKey)

	if _, err := conn.Write([]byte(req)); err != nil {
		log.Fatalf("handshake write failed: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		log.Fatalf("handshake failed: %s", statusLine)
	}

	// Read remaining HTTP headers
	for {
		line, _ := reader.ReadString('\n')
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	fmt.Printf("Connected as %s to room %s!\nType messages and press Enter:\n\n", user, order)

	// Background reader for incoming frames from server
	go func() {
		for {
			header := make([]byte, 2)
			if _, err := io.ReadFull(reader, header); err != nil {
				return
			}
			opcode := int(header[0] & 0x0F)
			lenByte := int(header[1] & 0x7F)

			var length int64
			if lenByte < 126 {
				length = int64(lenByte)
			} else if lenByte == 126 {
				ext := make([]byte, 2)
				_, _ = io.ReadFull(reader, ext)
				length = int64(binary.BigEndian.Uint16(ext))
			} else {
				ext := make([]byte, 8)
				_, _ = io.ReadFull(reader, ext)
				length = int64(binary.BigEndian.Uint64(ext))
			}

			payload := make([]byte, length)
			_, _ = io.ReadFull(reader, payload)

			if opcode == 0x1 { // Text frame
				var msg map[string]any
				if err := json.Unmarshal(payload, &msg); err == nil {
					switch msg["type"] {
					case "message":
						fmt.Printf("\n[%s]: %s\n> ", msg["from"], msg["text"])
					case "typing":
						fmt.Printf("\n... %s is typing ...\n> ", msg["from"])
					case "system":
						fmt.Printf("\n--- %s ---\n> ", msg["text"])
					}
				}
			}
		}
	}()

	// Read from stdin and send client->server masked frames
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			fmt.Print("> ")
			continue
		}

		payload, _ := json.Marshal(map[string]string{
			"type": "message",
			"text": text,
		})

		_ = writeClientFrame(conn, 0x1, payload)
		fmt.Print("> ")
	}
}

// writeClientFrame sends a client->server frame (MUST BE MASKED per RFC 6455)
func writeClientFrame(w io.Writer, opcode int, payload []byte) error {
	b0 := byte(0x80 | (opcode & 0x0F))
	n := len(payload)

	maskKey := make([]byte, 4)
	_, _ = rand.Read(maskKey)

	var header []byte
	if n < 126 {
		header = []byte{b0, byte(0x80 | n)}
	} else if n < 65536 {
		header = make([]byte, 4)
		header[0] = b0
		header[1] = byte(0x80 | 126)
		binary.BigEndian.PutUint16(header[2:4], uint16(n))
	} else {
		header = make([]byte, 10)
		header[0] = b0
		header[1] = byte(0x80 | 127)
		binary.BigEndian.PutUint64(header[2:10], uint64(n))
	}

	header = append(header, maskKey...)
	if _, err := w.Write(header); err != nil {
		return err
	}

	masked := make([]byte, len(payload))
	for i := 0; i < len(payload); i++ {
		masked[i] = payload[i] ^ maskKey[i%4]
	}

	_, err := w.Write(masked)
	return err
}
