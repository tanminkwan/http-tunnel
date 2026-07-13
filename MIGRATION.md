# frp → http-tunnel 마이그레이션 가이드 (단일 서비스 기준)

기존에 쓰시던 구조는 그대로 유지됩니다:

```
[외부 사용자] → [공개 서버: frps] ⇄ 터널 ⇄ [내부망: frpc] → [로컬 서비스]
```

바뀌는 건 frps/frpc의 **구현체**뿐입니다:

```
[외부 사용자] → [공개 서버: tunnel-server] ⇄ HTTP 롱폴링 ⇄ [내부망: tunnel-client] → [로컬 서비스]
```

## 설정값 1:1 매핑

기존에 쓰시던 `frps.toml` / `frpc.toml` (단일 proxy 기준)이 대략 이런 모습이었다면:

**frps.toml (공개 서버)**
```toml
bindPort = 7000
```

**frpc.toml (내부망)**
```toml
serverAddr = "1.2.3.4"
serverPort = 7000

[[proxies]]
name = "my-service"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000
```

이걸 http-tunnel 환경변수로 옮기면:

| frp 설정 | 역할 | http-tunnel 환경변수 | 설정 위치 |
|---|---|---|---|
| `frps.bindPort` | frpc가 접속해오는 컨트롤 포트 | `HTTP_PORT` | **server** |
| `frpc.serverAddr` | frps(공개 서버) 주소 | `SERVER_URL` (예: `https://1.2.3.4`) | **client** |
| `frpc.serverPort` | frps 접속 포트 | `SERVER_URL`에 포함 (예: `https://1.2.3.4:7000`) | **client** |
| `proxies[].remotePort` | 외부 사용자가 실제 접속하는 공개 포트 | `PUBLIC_PORT` | **server** |
| `proxies[].localIP` | 로컬 서비스 주소 | `LOCAL_HOST` | **client** |
| `proxies[].localPort` | 로컬 서비스 포트 | `LOCAL_PORT` | **client** |
| (frp의 `token` 인증) | 인증 토큰 | `AUTH_TOKEN` | **양쪽 동일하게** |

## 적용 예시

frp에서 SSH(22)를 원격 포트 6000으로 열어 쓰셨다면:

**공개 서버 (frps 자리 → tunnel-server):**
```bash
docker run -d \
  -e AUTH_TOKEN=원하는비밀값 \
  -e HTTP_PORT=7000 \
  -e PUBLIC_PORT=6000 \
  -p 6000:6000 \
  -p 7000:7000 \
  --name tunnel-server \
  http-tunnel-server
```

**내부망 (frpc 자리 → tunnel-client):**
```bash
docker run -d \
  -e SERVER_URL=https://1.2.3.4:7000 \
  -e AUTH_TOKEN=원하는비밀값 \
  -e LOCAL_HOST=127.0.0.1 \
  -e LOCAL_PORT=22 \
  --name tunnel-client \
  http-tunnel-client
```

이후 외부에서 `ssh -p 6000 user@1.2.3.4` 하던 것과 똑같이 접속하시면 됩니다.
바뀌는 건 전송 방식(HTTP 롱폴링 vs frp 자체 프로토콜)뿐, 앞단 사용 경험은 동일합니다.

## 주의할 점 (frp와 다른 부분)

- **단일 proxy 전용**입니다. frp처럼 하나의 클라이언트 연결에서 여러 서비스를
  동시에 여는 기능(`[[proxies]]` 여러 개)은 현재 지원하지 않습니다. 서비스를
  추가로 터널링하려면 `PUBLIC_PORT`/`LOCAL_PORT`를 바꿔 tunnel-server/client
  컨테이너를 서비스 개수만큼 별도로 띄우시면 됩니다 (포트마다 컨테이너 1쌍).
- frp의 `type = "tcp"`에 해당하는 순수 TCP 프록시만 지원합니다 (UDP, STCP,
  visitor 모드 등 frp의 고급 기능은 없음).
- 인증은 frp의 `token`보다 단순한 헤더 비교 방식입니다. 운영 환경에서는
  반드시 TLS(`https://`)와 함께 사용하세요.
