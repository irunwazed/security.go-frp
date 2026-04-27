saya mau membuat Fast Reverse Proxy untuk client dan server dengan teknologi golang

konsepnya mengikuti 
https://github.com/fatedier/frp


├── cmd/
│   ├── frps/main.go        ← entry point server
│   └── frpc/main.go        ← entry point client
├── internal/
│   ├── transport/          ← Layer 1: dial, listen, TLS
│   ├── mux/                ← Layer 2: yamux wrapper, conn pool
│   ├── proxy/              ← Layer 3: TCPProxy, HTTPProxy, dll
│   ├── protocol/           ← Layer 4: message types, encode/decode
│   └── config/             ← Layer 5: struct config, TOML parser
├── pkg/
│   └── util/               ← helper: logger, random ID, dll
└── go.mod


fokus pengembangan yaitu http dan ssh saja


untuk cara running 
- server
```
go run cmd/frps/main.go frps.toml
```
- client
```
go run cmd/frpc/main.go frpc.toml
```

