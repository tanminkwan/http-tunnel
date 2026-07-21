// http-tunnel client (Go, HTTP-only 버전)
//
// 서버로 순수 HTTP GET(롱폴링)/POST 요청만 보내며, 아웃바운드 연결만 사용합니다.
//
// 빌드:  go build -o tunnel-client main.go
// 실행:  SERVER_URL=http://서버IP:8080 AUTH_TOKEN=xxx LOCAL_PORT=3000 ./tunnel-client
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	frameOpen  = 0x01
	frameData  = 0x02
	frameClose = 0x03
)

var (
	serverURL = getEnv("SERVER_URL", "http://YOUR_SERVER_IP:8080")
	authToken = getEnv("AUTH_TOKEN", "change-me")
	localHost = getEnv("LOCAL_HOST", "127.0.0.1")
	localPort = getEnv("LOCAL_PORT", "3000")
	skipVerify = getEnv("SKIP_VERIFY", "false") == "true"

	httpClient = &http.Client{
		Timeout: 35 * time.Second, // poll 타임아웃(25s)보다 여유있게
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
		},
	}
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type frame struct {
	typ     byte
	connID  uint32
	payload []byte
}

func encodeFrame(typ byte, connID uint32, payload []byte) []byte {
	buf := make([]byte, 9+len(payload))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], connID)
	binary.BigEndian.PutUint32(buf[5:9], uint32(len(payload)))
	copy(buf[9:], payload)
	return buf
}

func decodeFrames(data []byte) []frame {
	var frames []frame
	offset := 0
	for offset+9 <= len(data) {
		typ := data[offset]
		connID := binary.BigEndian.Uint32(data[offset+1 : offset+5])
		length := binary.BigEndian.Uint32(data[offset+5 : offset+9])
		start := offset + 9
		end := start + int(length)
		if end > len(data) {
			break
		}
		frames = append(frames, frame{typ: typ, connID: connID, payload: data[start:end]})
		offset = end
	}
	return frames
}

var (
	connMu      sync.Mutex
	connections = make(map[uint32]net.Conn)
)

func sendFrame(typ byte, connID uint32, payload []byte) {
	buf := encodeFrame(typ, connID, payload)
	req, err := http.NewRequest("POST", serverURL+"/send", bytes.NewReader(buf))
	if err != nil {
		log.Println("요청 생성 오류:", err)
		return
	}
	req.Header.Set("X-Auth-Token", authToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Println("전송 오류:", err)
		return
	}
	resp.Body.Close()
}

func handleFrame(f frame) {
	switch f.typ {
	case frameOpen:
		conn, err := net.Dial("tcp", net.JoinHostPort(localHost, localPort))
		if err != nil {
			log.Printf("로컬 연결 실패 (connId=%d): %v", f.connID, err)
			sendFrame(frameClose, f.connID, nil)
			return
		}
		connMu.Lock()
		connections[f.connID] = conn
		connMu.Unlock()

		go func(connID uint32, c net.Conn) {
			buf := make([]byte, 32*1024)
			for {
				n, err := c.Read(buf)
				if n > 0 {
					payload := make([]byte, n)
					copy(payload, buf[:n])
					sendFrame(frameData, connID, payload)
				}
				if err != nil {
					break
				}
			}
			sendFrame(frameClose, connID, nil)
			connMu.Lock()
			delete(connections, connID)
			connMu.Unlock()
			c.Close()
		}(f.connID, conn)

	case frameData:
		connMu.Lock()
		conn, ok := connections[f.connID]
		connMu.Unlock()
		if ok {
			conn.Write(f.payload)
		}

	case frameClose:
		connMu.Lock()
		conn, ok := connections[f.connID]
		delete(connections, f.connID)
		connMu.Unlock()
		if ok {
			conn.Close()
		}
	}
}

func pollLoop() {
	log.Println("서버 폴링 시작:", serverURL)
	for {
		req, err := http.NewRequest("GET", serverURL+"/poll", nil)
		if err != nil {
			log.Println("요청 생성 오류:", err)
			time.Sleep(3 * time.Second)
			continue
		}
		req.Header.Set("X-Auth-Token", authToken)

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Println("poll 오류:", err)
			time.Sleep(3 * time.Second)
			continue
		}

		if resp.StatusCode == http.StatusNoContent {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Println("poll 응답 오류:", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(3 * time.Second)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Println("body 읽기 오류:", err)
			continue
		}

		for _, f := range decodeFrames(body) {
			handleFrame(f)
		}
	}
}

func main() {
	pollLoop()
}
