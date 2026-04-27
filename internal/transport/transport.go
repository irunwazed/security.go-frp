package transport

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Layer 1: pembungkus dial/listen.
// Sengaja dijaga tipis agar mudah diganti dengan TLS di kemudian hari.

func Listen(addr string, port int) (net.Listener, error) {
	host := addr
	if host == "" {
		host = "0.0.0.0"
	}
	return net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
}

func ListenPort(port int) (net.Listener, error) {
	return Listen("0.0.0.0", port)
}

func Dial(addr string, port int) (net.Conn, error) {
	return DialTimeout(addr, port, 10*time.Second)
}

func DialTimeout(addr string, port int, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.Dial("tcp", net.JoinHostPort(addr, fmt.Sprintf("%d", port)))
}

func DialContext(ctx context.Context, addr string, port int) (net.Conn, error) {
	d := net.Dialer{}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(addr, fmt.Sprintf("%d", port)))
}
