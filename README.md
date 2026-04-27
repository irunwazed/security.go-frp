# Tunnel — Fast Reverse Proxy sederhana

FRP minimal (mengikuti konsep [fatedier/frp](https://github.com/fatedier/frp)) yang
ditulis dalam Go. Fokus dukungan: **TCP (cocok untuk SSH)** dan **HTTP vhost**.

## Sistem secara singkat

```
[ user ]──TCP/HTTP──▶ frps ──yamux stream──▶ frpc ──TCP──▶ service lokal
                       ▲                       │
                       └────── 1 koneksi TCP ──┘
                              (control + work)
```

1. `frpc` dial ke `frps` lewat satu koneksi TCP, lalu dipromosikan menjadi sesi
   [yamux](https://github.com/hashicorp/yamux) — banyak stream di atas satu koneksi.
2. Stream pertama dipakai sebagai **control stream**: client kirim `Login`
   (token + daftar proxy), server balas `LoginResp`.
3. Untuk proxy `tcp` (mis. SSH), server membuka listener di `remote_port`. Tiap
   koneksi user → server membuka stream baru ke client, kirim
   `NewWorkConn{ProxyName}`, lalu menjembatani byte dua arah.
4. Untuk proxy `http`, server hanya membuka satu listener di `vhost_http_port`
   dan melakukan routing berdasarkan header `Host` (mendukung `custom_domains`
   atau `subdomain.<subdomain_host>`).

Lapisan kode mengikuti petunjuk pada [prompt.md](prompt.md):

```
cmd/frps        cmd/frpc            ← entry point
internal/transport                  ← Layer 1: dial / listen TCP
internal/mux                        ← Layer 2: pembungkus yamux
internal/proxy                      ← Layer 3: Manager, TCPProxy, HTTPProxy
internal/protocol                   ← Layer 4: pesan & framing
internal/config                     ← Layer 5: parser TOML
pkg/util                            ← logger, RandomID, Bridge
```

## Prasyarat

- Go ≥ 1.23
- Sistem operasi: Linux / macOS / Windows

## Install

Clone repo ini lalu unduh dependensi:

```bash
git clone <repo-url> tunnel
cd tunnel
go mod tidy
```

## Build

Build kedua biner ke folder `bin/`:

```bash
# Linux / macOS
mkdir -p bin
go build -o bin/frps ./cmd/frps
go build -o bin/frpc ./cmd/frpc
```

```powershell
# Windows
mkdir bin -ErrorAction SilentlyContinue
go build -o bin/frps.exe ./cmd/frps
go build -o bin/frpc.exe ./cmd/frpc
```

Build untuk produksi (binari lebih ramping, tanpa simbol debug):

```bash
go build -trimpath -ldflags="-s -w" -o bin/frps ./cmd/frps
go build -trimpath -ldflags="-s -w" -o bin/frpc ./cmd/frpc
```

Cross-compile (contoh: build Linux dari Windows):

```bash
GOOS=linux GOARCH=amd64 go build -o bin/frps-linux ./cmd/frps
GOOS=linux GOARCH=amd64 go build -o bin/frpc-linux ./cmd/frpc
```

## Cara menjalankan — Mode Dev

Dijalankan langsung dengan `go run`, pakai contoh TOML yang sudah disediakan
([frps.toml](frps.toml), [frpc.toml](frpc.toml)).

Terminal 1 — server:

```bash
go run ./cmd/frps frps.toml
```

Terminal 2 — client:

```bash
go run ./cmd/frpc frpc.toml
```

Uji cepat:

```bash
# SSH lewat tunnel (sesuai contoh frpc.toml: remote_port = 6000)
ssh -p 6000 user@127.0.0.1

# HTTP vhost (sesuai contoh: custom_domains = ["web.example.com"])
curl --resolve web.example.com:8080:127.0.0.1 http://web.example.com:8080/
```

Tip: client otomatis reconnect dengan exponential backoff 1 s → 30 s, jadi
kamu boleh restart server tanpa restart client.

## Cara menjalankan — Mode Prod

1. **Build biner** (lihat bagian Build di atas).
2. **Salin biner + config** ke server target.
3. **Edit `frps.toml`** di server, **`frpc.toml`** di host yang akan di-expose.
   Wajib ganti `auth_token` ke nilai acak.
4. Jalankan biner. Contoh systemd unit untuk server:

   `/etc/systemd/system/frps.service`
   ```ini
   [Unit]
   Description=Tunnel frps
   After=network.target

   [Service]
   Type=simple
   User=frp
   WorkingDirectory=/opt/frp
   ExecStart=/opt/frp/frps /opt/frp/frps.toml
   Restart=on-failure
   RestartSec=3

   [Install]
   WantedBy=multi-user.target
   ```

   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now frps
   sudo journalctl -u frps -f
   ```

   Unit serupa untuk client (`frpc.service`) dengan `ExecStart=/opt/frp/frpc /opt/frp/frpc.toml`.

5. **Buka firewall** untuk port yang relevan di server: `bind_port` (default 7000),
   `vhost_http_port` (default 8080), serta tiap `remote_port` proxy TCP
   (mis. 6000 untuk SSH).

### Catatan keamanan

- **Selalu set `auth_token`** di kedua sisi. Token kosong = tanpa auth.
- Versi ini belum mengaktifkan TLS pada control connection. Untuk produksi,
  tempatkan server di balik VPN / jaringan terpercaya, atau bungkus port
  `bind_port` dengan tunneling TLS terpisah (mis. stunnel, WireGuard).
- HTTP routing hanya membaca header `Host` — gunakan ingress eksternal yang
  meng-handle TLS (mis. Caddy / nginx) bila butuh HTTPS.

## Konfigurasi singkat

`frps.toml` (server):

```toml
bind_addr       = "0.0.0.0"
bind_port       = 7000
vhost_http_port = 8080         # 0 / hapus baris ini untuk nonaktifkan HTTP
auth_token      = "ganti-saya"
subdomain_host  = "example.com"
```

`frpc.toml` (client) — beberapa proxy bisa didaftarkan sekaligus:

```toml
server_addr = "127.0.0.1"
server_port = 7000
auth_token  = "ganti-saya"

[[proxies]]
name        = "ssh"
type        = "tcp"
local_addr  = "127.0.0.1"
local_port  = 22
remote_port = 6000

[[proxies]]
name           = "web"
type           = "http"
local_addr     = "127.0.0.1"
local_port     = 8000
custom_domains = ["web.example.com"]
```

Tipe yang didukung: `tcp` dan `http`. `tcp` butuh `remote_port`; `http` butuh
salah satu dari `custom_domains` atau `subdomain` (kombinasikan dengan
`subdomain_host` di server).
