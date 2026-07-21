# --- 빌드 스테이지 ---
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY main.go .

# CA 인증서: 클라이언트가 https:// 서버로 접속할 때 TLS 검증에 필요
RUN apk add --no-cache ca-certificates

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tunnel-client main.go

# --- 실행 스테이지 (최소 이미지) ---
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /src/tunnel-client /tunnel-client

ENV SERVER_URL=https://YOUR_SERVER_IP:8080 \
    AUTH_TOKEN=change-me \
    LOCAL_HOST=127.0.0.1 \
    LOCAL_PORT=3000 \
    SKIP_VERIFY=false

ENTRYPOINT ["/tunnel-client"]
