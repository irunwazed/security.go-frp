package mux

import (
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

// Layer 2: pembungkus tipis untuk yamux.

func defaultConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.AcceptBacklog = 256
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 25 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	// Buang log internal yamux agar output bersih.
	// Catatan: yamux mewajibkan tepat salah satu dari Logger / LogOutput
	// terisi — keduanya nil akan membuat VerifyConfig gagal.
	cfg.LogOutput = io.Discard
	cfg.Logger = nil
	return cfg
}

// Server membungkus yamux.Server.
func Server(conn net.Conn) (*yamux.Session, error) {
	return yamux.Server(conn, defaultConfig())
}

// Client membungkus yamux.Client.
func Client(conn net.Conn) (*yamux.Session, error) {
	return yamux.Client(conn, defaultConfig())
}
