package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Protokol kontrol antara frpc dan frps.
//
// Setiap pesan memiliki bingkai 5 byte di depan:
//   - 1 byte: tipe pesan
//   - 4 byte: panjang payload (big-endian uint32)
// disusul payload JSON.

type MsgType byte

const (
	TypeLogin       MsgType = 1
	TypeLoginResp   MsgType = 2
	TypeNewWorkConn MsgType = 3
	TypePing        MsgType = 4
	TypePong        MsgType = 5
)

const maxPayload = 1 << 20 // 1 MiB

// Login dikirim client setelah membuka kontrol koneksi/stream.
type Login struct {
	Token   string        `json:"token"`
	Proxies []ProxyEntry  `json:"proxies"`
	Version string        `json:"version"`
}

// ProxyEntry adalah satu definisi proxy yang dikirim ke server.
type ProxyEntry struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	RemotePort    int      `json:"remote_port,omitempty"`
	CustomDomains []string `json:"custom_domains,omitempty"`
	Subdomain     string   `json:"subdomain,omitempty"`
}

// LoginResp dikirim server membalas Login.
type LoginResp struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// RunID identifier sesi.
	RunID string `json:"run_id,omitempty"`
}

// NewWorkConn dikirim server di awal stream kerja baru,
// memberitahu client proxy mana yang harus melayani.
type NewWorkConn struct {
	ProxyName string `json:"proxy_name"`
}

func WriteMsg(w io.Writer, t MsgType, v interface{}) error {
	var data []byte
	if v != nil {
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
	}
	if len(data) > maxPayload {
		return fmt.Errorf("payload terlalu besar: %d", len(data))
	}
	head := make([]byte, 5)
	head[0] = byte(t)
	binary.BigEndian.PutUint32(head[1:], uint32(len(data)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func ReadMsg(r io.Reader, v interface{}) (MsgType, error) {
	head := make([]byte, 5)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, err
	}
	t := MsgType(head[0])
	size := binary.BigEndian.Uint32(head[1:])
	if size > maxPayload {
		return t, fmt.Errorf("payload terlalu besar: %d", size)
	}
	if size == 0 {
		return t, nil
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return t, err
	}
	if v != nil {
		if err := json.Unmarshal(data, v); err != nil {
			return t, fmt.Errorf("unmarshal: %w", err)
		}
	}
	return t, nil
}
