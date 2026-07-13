// http-tunnel server (Go, HTTP-only 버전)
//
// WebSocket 없이 순수 HTTP GET(롱폴링)/POST 요청만으로 동작하는 역방향 터널 서버입니다.
//
// 엔드포인트:
//   GET  /poll  -> 클라이언트가 반복 호출(롱폴링). 서버->클라이언트 프레임 응답.
//   POST /send  -> 클라이언트가 로컬 응답 데이터를 프레임으로 담아 전송.
//
// 빌드:  go build -o tunnel-server main.go
// 실행:  AUTH_TOKEN=xxx HTTP_PORT=8080 PUBLIC_PORT=9000 ./tunnel-server
package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	frameOpen  = 0x01
	frameData  = 0x02
	frameClose = 0x03
)

var (
	httpPort   = getEnv("HTTP_PORT", "8080")
	publicPort = getEnv("PUBLIC_PORT", "9000")
	authToken  = getEnv("AUTH_TOKEN", "change-me")
	pollTimeout = 25 * time.Second
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ---- 프레임 인코딩/디코딩 ----
// [1바이트 type][4바이트 connId][4바이트 length][payload]

type frame struct {
	typ    byte
	connID uint32
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

// ---- 상태 ----

var (
	connMu      sync.Mutex
	connections = make(map[uint32]net.Conn)
	nextConnID  uint32

	outboxMu sync.Mutex
	outbox   [][]byte
	notify   = make(chan struct{}, 1)
)

func pushToClient(buf []byte) {
	outboxMu.Lock()
	outbox = append(outbox, buf)
	outboxMu.Unlock()

	select {
	case notify <- struct{}{}:
	default:
	}
}

func drainOutbox() []byte {
	outboxMu.Lock()
	defer outboxMu.Unlock()
	if len(outbox) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, f := range outbox {
		buf.Write(f)
	}
	outbox = nil
	return buf.Bytes()
}

func checkAuth(r *http.Request) bool {
	return r.Header.Get("X-Auth-Token") == authToken
}

func pollHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	data := drainOutbox()
	if data == nil {
		select {
		case <-notify:
			data = drainOutbox()
		case <-time.After(pollTimeout):
		}
	}

	if data == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

func sendHandler(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, f := range decodeFrames(body) {
		connMu.Lock()
		sock, ok := connections[f.connID]
		connMu.Unlock()
		if !ok {
			continue
		}
		switch f.typ {
		case frameData:
			sock.Write(f.payload)
		case frameClose:
			sock.Close()
			connMu.Lock()
			delete(connections, f.connID)
			connMu.Unlock()
		}
	}
	w.WriteHeader(http.StatusOK)
}

func handleExternalConn(sock net.Conn) {
	connID := atomic.AddUint32(&nextConnID, 1)
	connMu.Lock()
	connections[connID] = sock
	connMu.Unlock()

	pushToClient(encodeFrame(frameOpen, connID, nil))

	buf := make([]byte, 32*1024)
	for {
		n, err := sock.Read(buf)
		if n > 0 {
			payload := make([]byte, n)
			copy(payload, buf[:n])
			pushToClient(encodeFrame(frameData, connID, payload))
		}
		if err != nil {
			break
		}
	}

	pushToClient(encodeFrame(frameClose, connID, nil))
	connMu.Lock()
	delete(connections, connID)
	connMu.Unlock()
	sock.Close()
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/poll", pollHandler)
	mux.HandleFunc("/send", sendHandler)

	go func() {
		log.Printf("API(HTTP) 포트 %s 대기 중 (/poll, /send)", httpPort)
		if err := http.ListenAndServe(":"+httpPort, mux); err != nil {
			log.Fatal(err)
		}
	}()

	listener, err := net.Listen("tcp", ":"+publicPort)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("공개 포트 %s 에서 외부 연결 대기 중", publicPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("accept 오류:", err)
			continue
		}
		go handleExternalConn(conn)
	}
}
