package proxy

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/irunwazed/tunnel/internal/protocol"
	"github.com/irunwazed/tunnel/pkg/util"
)

// WorkSession adalah abstraksi sesi multiplex client.
// *yamux.Session memenuhi interface ini.
type WorkSession interface {
	Open() (net.Conn, error)
	IsClosed() bool
}

// Manager mengelola registrasi proxy dari semua client yang terhubung.
type Manager struct {
	mu sync.Mutex

	tcpBindings map[int]*tcpBinding // key: remote port

	vhostListener net.Listener
	vhostHost     string                   // contoh: "example.com"
	httpBindings  map[string]*httpBinding  // key: host (lowercase)
}

type tcpBinding struct {
	proxy    protocol.ProxyEntry
	session  WorkSession
	listener net.Listener
}

type httpBinding struct {
	proxy   protocol.ProxyEntry
	session WorkSession
}

func NewManager(vhostListener net.Listener, vhostHost string) *Manager {
	m := &Manager{
		tcpBindings:   make(map[int]*tcpBinding),
		httpBindings:  make(map[string]*httpBinding),
		vhostListener: vhostListener,
		vhostHost:     strings.ToLower(vhostHost),
	}
	if vhostListener != nil {
		go m.serveHTTP()
	}
	return m
}

// Register mendaftarkan satu set proxy untuk satu sesi.
// Atomic: bila ada satu yang gagal, tidak ada yang terdaftar.
func (m *Manager) Register(session WorkSession, proxies []protocol.ProxyEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validasi konflik dulu.
	for _, p := range proxies {
		switch p.Type {
		case "tcp":
			if p.RemotePort <= 0 {
				return fmt.Errorf("proxy %q: remote_port wajib", p.Name)
			}
			if _, exists := m.tcpBindings[p.RemotePort]; exists {
				return fmt.Errorf("port %d sudah dipakai (proxy %q)", p.RemotePort, p.Name)
			}
		case "http":
			if m.vhostListener == nil {
				return fmt.Errorf("server tidak punya vhost_http_port; proxy http tidak bisa")
			}
			for _, h := range hostsForProxy(p, m.vhostHost) {
				if _, exists := m.httpBindings[h]; exists {
					return fmt.Errorf("host %q sudah dipakai (proxy %q)", h, p.Name)
				}
			}
		default:
			return fmt.Errorf("proxy %q: tipe %q tidak didukung", p.Name, p.Type)
		}
	}

	// Apply.
	var newTCP []int
	var newHosts []string
	rollback := func() {
		for _, port := range newTCP {
			if b := m.tcpBindings[port]; b != nil && b.listener != nil {
				_ = b.listener.Close()
			}
			delete(m.tcpBindings, port)
		}
		for _, h := range newHosts {
			delete(m.httpBindings, h)
		}
	}

	for _, p := range proxies {
		switch p.Type {
		case "tcp":
			ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", p.RemotePort))
			if err != nil {
				rollback()
				return fmt.Errorf("listen :%d (%s): %w", p.RemotePort, p.Name, err)
			}
			b := &tcpBinding{proxy: p, session: session, listener: ln}
			m.tcpBindings[p.RemotePort] = b
			newTCP = append(newTCP, p.RemotePort)
			go m.serveTCP(b)
		case "http":
			for _, h := range hostsForProxy(p, m.vhostHost) {
				m.httpBindings[h] = &httpBinding{proxy: p, session: session}
				newHosts = append(newHosts, h)
				util.Infof("http proxy %q terdaftar untuk host %q", p.Name, h)
			}
		}
	}
	return nil
}

// Unregister membersihkan semua proxy milik sesi.
func (m *Manager) Unregister(session WorkSession) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for port, b := range m.tcpBindings {
		if b.session == session {
			_ = b.listener.Close()
			delete(m.tcpBindings, port)
		}
	}
	for h, b := range m.httpBindings {
		if b.session == session {
			delete(m.httpBindings, h)
		}
	}
}

func hostsForProxy(p protocol.ProxyEntry, vhostHost string) []string {
	var out []string
	for _, d := range p.CustomDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			out = append(out, d)
		}
	}
	if p.Subdomain != "" && vhostHost != "" {
		out = append(out, strings.ToLower(p.Subdomain)+"."+vhostHost)
	}
	return out
}

// openWorkConn membuka stream kerja ke client dan mengirim header NewWorkConn.
func openWorkConn(session WorkSession, proxyName string) (net.Conn, error) {
	if session.IsClosed() {
		return nil, fmt.Errorf("sesi sudah tertutup")
	}
	stream, err := session.Open()
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	if err := protocol.WriteMsg(stream, protocol.TypeNewWorkConn, &protocol.NewWorkConn{
		ProxyName: proxyName,
	}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("kirim NewWorkConn: %w", err)
	}
	return stream, nil
}
