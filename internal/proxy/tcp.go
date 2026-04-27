package proxy

import (
	"errors"
	"net"

	"github.com/irunwazed/tunnel/pkg/util"
)

// serveTCP melayani satu listener TCP yang dimiliki sebuah binding.
func (m *Manager) serveTCP(b *tcpBinding) {
	util.Infof("tcp proxy %q listen di port %d", b.proxy.Name, b.proxy.RemotePort)
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				util.Infof("tcp proxy %q tutup", b.proxy.Name)
				return
			}
			util.Warnf("tcp proxy %q accept: %v", b.proxy.Name, err)
			return
		}
		go m.handleTCP(b, conn)
	}
}

func (m *Manager) handleTCP(b *tcpBinding, conn net.Conn) {
	defer conn.Close()

	stream, err := openWorkConn(b.session, b.proxy.Name)
	if err != nil {
		util.Warnf("tcp %q work conn: %v", b.proxy.Name, err)
		return
	}
	defer stream.Close()

	util.Infof("tcp %q: %s ↔ stream", b.proxy.Name, conn.RemoteAddr())
	util.Bridge(conn, stream)
}
