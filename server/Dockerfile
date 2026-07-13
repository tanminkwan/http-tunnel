# --- 빌드 스테이지 ---
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY main.go .

# CGO 비활성화 + 정적 바이너리로 빌드해야 scratch 이미지에서 실행 가능
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tunnel-server main.go

# --- 실행 스테이지 (최소 이미지) ---
FROM scratch

COPY --from=builder /src/tunnel-server /tunnel-server

# HTTP_PORT: 클라이언트가 붙는 API 포트 / PUBLIC_PORT: 외부 사용자가 붙는 포트
ENV AUTH_TOKEN=change-me \
    HTTP_PORT=8080 \
    PUBLIC_PORT=9000

EXPOSE 8080 9000

ENTRYPOINT ["/tunnel-server"]
