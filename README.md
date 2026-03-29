# relayd

> Self-hosted reverse tunnel service. Expose your localhost to the internet — like ngrok, but fully under your control.

![Tests](https://github.com/dlc-01/relayd/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.25-blue)
![License](https://img.shields.io/badge/license-MIT-green)

---

## Try the live demo

Want to see it in action without setting up a server?

**Demo access** — credentials available in resume. Setup takes ~30 seconds.

### Option 1 — Download binary (no Go required)
```bash
# linux/amd64
curl -L https://github.com/dlc-01/relayd/releases/latest/download/relayd-linux-amd64 -o relayd
chmod +x relayd
./relayd client ...

# macOS (Apple Silicon)
curl -L https://github.com/dlc-01/relayd/releases/latest/download/relayd-darwin-arm64 -o relayd
chmod +x relayd
./relayd client ...

# macOS (Intel)
curl -L https://github.com/dlc-01/relayd/releases/latest/download/relayd-darwin-amd64 -o relayd
chmod +x relayd
./relayd client ...

# Windows (PowerShell)
Invoke-WebRequest -Uri https://github.com/dlc-01/relayd/releases/latest/download/relayd-windows-amd64.exe -OutFile relayd.exe
.\relayd.exe client ...
```

### Option 2 — Build from source (Go 1.25+)
```bash
go install github.com/dlc-01/relayd/cmd/relayd@latest
```

### Connect
```bash
# start any local service
python3 -m http.server 8080

# connect to demo server
./relayd client \
  --server SERVER_ADDR \
  --token YOUR_TOKEN \
  --tunnel myapp:host:myapp.DOMAIN:127.0.0.1:8080
```


Your local service is now live at `https://myapp.DOMAIN`

---

## How it works

```
Internet  ──────────────▶  VPS (relayd server)
                               :80/:443   HTTP/HTTPS routing by Host
                               :7000      control plane  (TLS encrypted)
                               :7001      data plane     (TLS encrypted)
                               :7002      admin API      (localhost only)
                                   │
                            encrypted tunnel
                                   │
                           Your machine
                               relayd client
                                   │
                               localhost:8080
```

1. Client connects to server via **encrypted TLS tunnel**
2. Server issues a **24h session token** — master token never travels again
3. Incoming HTTP/HTTPS requests are routed by **Host header** to the right client
4. Client **auto-reconnects** with exponential backoff if connection drops
5. **Certificate pinning** protects against MITM — client pins server cert on first connect

---

## Why relayd?

| Feature | ngrok free | relayd |
|---------|-----------|--------|
| Custom domain | ❌ | ✅ |
| Unlimited tunnels | ❌ | ✅ |
| No rate limiting | ❌ | ✅ |
| Self-hosted | ❌ | ✅ |
| Open source | ❌ | ✅ |
| TLS encryption | ✅ | ✅ |
| Token auth | ✅ | ✅ |

---

## Tunnel formats

```bash
# HTTP routing by Host header
--tunnel app:host:app.example.com:127.0.0.1:8080

# HTTPS (TLS terminated on server)
--tunnel app:https:app.example.com:127.0.0.1:8080

# Domain aliasing — multiple domains, one tunnel
--tunnel app:host:app.example.com|alias.com:127.0.0.1:8080

# Raw TCP tunnel
--tunnel db:10001:127.0.0.1:5432
```

---

## Self-hosted setup

### Prerequisites

- VPS with public IP (tested on Ubuntu 22.04)
- Domain pointing to VPS IP
- Ports open: `80`, `443`, `7000`, `7001`
- Go 1.25+

### 1. Get SSL certificate

```bash
apt install certbot python3-certbot-dns-cloudflare

certbot certonly \
  --dns-cloudflare \
  --dns-cloudflare-credentials ~/.cloudflare.ini \
  -d "*.YOUR_DOMAIN" \
  -d "YOUR_DOMAIN"
```

Wildcard certificate `*.YOUR_DOMAIN` covers all subdomains automatically.

### 2. Build and deploy server

```bash
git clone https://github.com/dlc-01/relayd
cd relayd

GOOS=linux GOARCH=amd64 go build -o relayd-server ./cmd/server
scp relayd-server root@YOUR_VPS:/opt/relayd/
```

### 3. Create systemd service

`/etc/systemd/system/relayd-server.service`:

```ini
[Unit]
Description=Relayd Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/relayd
ExecStart=/opt/relayd/relayd-server
Restart=on-failure
RestartSec=5s

Environment=RELAYD_CONTROL_ADDR=0.0.0.0:7000
Environment=RELAYD_DATA_ADDR=0.0.0.0:7001
Environment=RELAYD_HTTP_ADDR=0.0.0.0:80
Environment=RELAYD_TLS_ADDR=0.0.0.0:443
Environment=RELAYD_TLS_CERT=/etc/letsencrypt/live/YOUR_DOMAIN/fullchain.pem
Environment=RELAYD_TLS_KEY=/etc/letsencrypt/live/YOUR_DOMAIN/privkey.pem
Environment=RELAYD_TLS_DOMAIN=YOUR_DOMAIN
Environment=RELAYD_TOKEN=YOUR_MASTER_TOKEN
Environment=RELAYD_SESSION_TTL=24h
Environment=RELAYD_MIN_PORT=10000
Environment=RELAYD_MAX_PORT=60000

# optional: telegram notifications
Environment=RELAYD_TG_TOKEN=YOUR_BOT_TOKEN
Environment=RELAYD_TG_CHAT_ID=YOUR_CHAT_ID

StandardOutput=journal
StandardError=journal
SyslogIdentifier=relayd-server

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable relayd-server
systemctl start relayd-server
```

### 4. Open firewall

```bash
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 7000/tcp
ufw allow 7001/tcp
# port 7002 (admin API) stays closed — bound to 127.0.0.1 only
```

### 5. Verify

```bash
journalctl -u relayd-server -o cat | jq '.'
# should show: auth enabled, control tls ready, listening on all ports
```

### Multiple domains

```bash
certbot certonly --dns-cloudflare -d "*.another.com" -d "another.com"
```

```ini
# add to systemd — no code changes needed
Environment=RELAYD_TLS_DOMAINS=example.com:/etc/letsencrypt/live/example.com/fullchain.pem:/etc/letsencrypt/live/example.com/privkey.pem,another.com:/etc/letsencrypt/live/another.com/fullchain.pem:/etc/letsencrypt/live/another.com/privkey.pem
```

Server picks the right certificate by SNI automatically.

---

## CLI

```bash
# install
go install github.com/dlc-01/relayd/cmd/relayd@latest

# client
relayd client \
  --server YOUR_VPS:7000 \
  --token YOUR_TOKEN \
  --tunnel app:host:app.YOUR_DOMAIN:127.0.0.1:8080

# multiple tunnels
relayd client \
  --server YOUR_VPS:7000 \
  --token YOUR_TOKEN \
  --tunnel web:host:web.YOUR_DOMAIN:127.0.0.1:3000 \
  --tunnel api:host:api.YOUR_DOMAIN:127.0.0.1:8080

# server
relayd server --token SECRET --tg-token BOT_TOKEN --tg-chat CHAT_ID

# token management (run on VPS)
relayd token issue --master SECRET --label vasya --ttl 24h
relayd token list  --master SECRET
relayd token revoke TOKEN --master SECRET

# version
relayd version
```

All flags have env variable fallbacks.

---

## Configuration reference

### Server

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--control` | `RELAYD_CONTROL_ADDR` | `0.0.0.0:7000` | Control plane address |
| `--data` | `RELAYD_DATA_ADDR` | `0.0.0.0:7001` | Data plane address |
| `--http` | `RELAYD_HTTP_ADDR` | `0.0.0.0:80` | HTTP routing |
| `--tls` | `RELAYD_TLS_ADDR` | `0.0.0.0:443` | HTTPS routing |
| `--token` | `RELAYD_TOKEN` | — | Master token (auth disabled if not set) |
| `--session-ttl` | `RELAYD_SESSION_TTL` | `24h` | Temp token TTL |
| `--tg-token` | `RELAYD_TG_TOKEN` | — | Telegram bot token |
| `--tg-chat` | `RELAYD_TG_CHAT_ID` | — | Telegram chat ID |
| `--dev` | `RELAYD_DEV` | `false` | Development logging |

### Client

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--server` | `RELAYD_SERVER_CONTROL` | `localhost:7000` | Control server |
| `--data` | `RELAYD_SERVER_DATA` | `localhost:7001` | Data server |
| `--token` | `RELAYD_TOKEN` | — | Auth token |
| `--tunnel` | `RELAYD_TUNNELS` | — | Tunnel definitions |
| `--pin-file` | `RELAYD_PIN_FILE` | `~/.relayd/server.pin` | Certificate pin |
| `--session-file` | `RELAYD_SESSION_FILE` | `~/.relayd/session.json` | Session token |

---

## Security

- **TLS** — all client↔server traffic is encrypted
- **Certificate pinning** — client pins server certificate on first connect (TOFU model)
- **Token auth** — master token stays local, server issues short-lived 24h session tokens
- **Admin API** — bound to `127.0.0.1:7002`, not accessible from outside

---

## Benchmarks

Measured on MacBook Air M2 (2022), 16GB, macOS Sequoia 15.3.1, Go 1.25, `go test -bench=. -benchmem -benchtime=5s`.

| Benchmark | ops/s | ns/op | throughput | allocs/op |
|-----------|------:|------:|------------|----------:|
| Tunnel_Throughput | 78 806 | 76 059 | **53.85 MB/s** | 2 |
| Tunnel_Latency | 91 076 | 67 396 | ~0.07ms RTT | 2 |
| Tunnel_ConcurrentConns | 188 319 | 30 358 | — | 2 |
| Server_HTTPRouting | 57 111 | 110 701 | — | 69 |
| Server_Register | 9 303 | 781 251 | — | 1165 |
| Server_HostLookup (100 tunnels) | 59 667 | 103 225 | — | 69 |

CPU profile: 98% syscalls (network IO + TLS) — application code is not the bottleneck.

Notable:
- ~54 MB/s through double TLS (client→server→backend)
- ~0.07ms round-trip latency
- Concurrent connections 2x faster than single — goroutine scheduler works efficiently
- Host lookup with 100 active tunnels same speed as 1 — O(1) map lookup
- Register dominated by TLS handshake — expected

Benchmarks are local measurements and mainly reflect relative overhead of the implementation.
---

## Project structure

```
relayd/
├── cmd/
│   ├── client/          systemd entrypoint
│   ├── server/          systemd entrypoint
│   └── relayd/          cobra CLI
│       └── cmd/
├── internal/
│   ├── auth/            token management (master → temp session)
│   ├── client/          tunnel client, reconnect, heartbeat
│   ├── config/          env/flag configuration
│   ├── httpparse/       HTTP Host header parsing
│   ├── notify/          Notifier interface + Telegram
│   ├── pin/             certificate pinning
│   ├── portcheck/       port validation
│   ├── proto/           wire protocol (JSON newline-delimited)
│   ├── server/          tunnel server + admin API
│   ├── session/         session token persistence
│   └── tlscerts/        TLS certificate management
└── .github/
    └── workflows/
        └── ci.yml       test + deploy on push to main
```

---

## Development

```bash
# run tests
go test ./... -race

# run server locally
RELAYD_DEV=true go run ./cmd/server

# run client locally
RELAYD_DEV=true \
RELAYD_SERVER_CONTROL=localhost:7000 \
RELAYD_TUNNELS=app:host:app.localhost:127.0.0.1:8080 \
go run ./cmd/client
```

---

## CI/CD

GitHub Actions on every push:
- `go test ./... -race`
- builds binaries for linux/amd64
- deploys to VPS on push to `main` (atomic swap — no downtime)

Required secrets: `SSH_PRIVATE_KEY`, `SSH_HOST`, `SSH_USER`, `TLS_DOMAIN`, `RELAYD_TOKEN`, `RELAYD_TG_TOKEN`, `RELAYD_TG_CHAT_ID`

---

## Roadmap

- [ ] UDP tunnels
- [ ] QUIC transport
- [ ] HTTP/3 public listener
