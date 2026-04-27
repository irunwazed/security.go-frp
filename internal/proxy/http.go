package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/irunwazed/tunnel/pkg/util"
)

// serveHTTP menerima koneksi HTTP di vhost listener dan
// melakukan routing berdasarkan header Host.
func (m *Manager) serveHTTP() {
	util.Infof("vhost http listen di %s", m.vhostListener.Addr())
	for {
		conn, err := m.vhostListener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				util.Infof("vhost http tutup")
				return
			}
			util.Warnf("vhost http accept: %v", err)
			return
		}
		go m.handleHTTP(conn)
	}
}

func (m *Manager) handleHTTP(conn net.Conn) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	host, prefix, err := peekHTTPHost(conn)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		util.Warnf("vhost http parse: %v", err)
		return
	}
	host = normalizeHost(host)

	m.mu.Lock()
	b, ok := m.httpBindings[host]
	m.mu.Unlock()
	if !ok {
		util.Warnf("vhost http: host %q tidak terdaftar", host)
		body := "frps: host tidak dikenal\n"
		_, _ = conn.Write([]byte(
			"HTTP/1.1 404 Not Found\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"Content-Length: " + strconv.Itoa(len(body)) + "\r\n" +
				"Connection: close\r\n\r\n" + body))
		return
	}

	stream, err := openWorkConn(b.session, b.proxy.Name)
	if err != nil {
		util.Warnf("http %q work conn: %v", b.proxy.Name, err)
		return
	}
	defer stream.Close()

	if _, err := stream.Write(prefix); err != nil {
		util.Warnf("http %q write prefix: %v", b.proxy.Name, err)
		return
	}

	util.Infof("http %q: %s host=%s ↔ stream", b.proxy.Name, conn.RemoteAddr(), host)
	util.Bridge(conn, stream)
}

// peekHTTPHost membaca cukup byte dari conn untuk mengetahui header Host,
// lalu mengembalikan host beserta seluruh byte yang sudah dibaca dari conn
// agar bisa diteruskan apa adanya ke backend.
func peekHTTPHost(conn net.Conn) (host string, prefix []byte, err error) {
	var buf bytes.Buffer
	tee := io.TeeReader(conn, &buf)
	br := bufio.NewReader(tee)
	req, err := http.ReadRequest(br)
	if err != nil {
		return "", buf.Bytes(), err
	}
	return req.Host, buf.Bytes(), nil
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}
