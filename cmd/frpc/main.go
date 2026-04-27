package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/irunwazed/tunnel/internal/config"
	"github.com/irunwazed/tunnel/internal/mux"
	"github.com/irunwazed/tunnel/internal/protocol"
	"github.com/irunwazed/tunnel/internal/transport"
	"github.com/irunwazed/tunnel/pkg/util"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: frpc <config.toml>")
		os.Exit(2)
	}

	cfg, err := config.LoadClient(os.Args[1])
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		util.Infof("shutting down")
		cancel()
	}()

	backoff := time.Second
	for {
		err := runOnce(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			util.Warnf("sesi berakhir: %v", err)
		} else {
			util.Infof("sesi berakhir")
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func runOnce(ctx context.Context, cfg *config.ClientConfig) error {
	util.Infof("connect ke %s:%d", cfg.ServerAddr, cfg.ServerPort)
	conn, err := transport.Dial(cfg.ServerAddr, cfg.ServerPort)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	sess, err := mux.Client(conn)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("yamux client: %w", err)
	}
	defer sess.Close()

	go func() {
		<-ctx.Done()
		_ = sess.Close()
	}()

	ctrl, err := sess.OpenStream()
	if err != nil {
		return fmt.Errorf("open control stream: %w", err)
	}

	var entries []protocol.ProxyEntry
	for _, p := range cfg.Proxies {
		entries = append(entries, protocol.ProxyEntry{
			Name:          p.Name,
			Type:          p.Type,
			RemotePort:    p.RemotePort,
			CustomDomains: p.CustomDomains,
			Subdomain:     p.Subdomain,
		})
	}
	if err := protocol.WriteMsg(ctrl, protocol.TypeLogin, &protocol.Login{
		Token:   cfg.AuthToken,
		Proxies: entries,
		Version: "0.1.0",
	}); err != nil {
		return fmt.Errorf("kirim login: %w", err)
	}

	var resp protocol.LoginResp
	mt, err := protocol.ReadMsg(ctrl, &resp)
	if err != nil {
		return fmt.Errorf("baca login resp: %w", err)
	}
	if mt != protocol.TypeLoginResp {
		return fmt.Errorf("tipe pesan tak terduga: %v", mt)
	}
	if !resp.OK {
		return fmt.Errorf("login ditolak: %s", resp.Error)
	}
	util.Infof("login OK (run_id=%s, %d proxy)", resp.RunID, len(entries))

	proxies := make(map[string]config.ProxyConfig, len(cfg.Proxies))
	for _, p := range cfg.Proxies {
		proxies[p.Name] = p
	}

	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || sess.IsClosed() {
				return nil
			}
			return fmt.Errorf("accept stream: %w", err)
		}
		go handleWorkStream(stream, proxies)
	}
}

func handleWorkStream(stream net.Conn, proxies map[string]config.ProxyConfig) {
	defer stream.Close()

	var nw protocol.NewWorkConn
	mt, err := protocol.ReadMsg(stream, &nw)
	if err != nil {
		util.Warnf("baca NewWorkConn: %v", err)
		return
	}
	if mt != protocol.TypeNewWorkConn {
		util.Warnf("tipe pesan tak terduga di work stream: %v", mt)
		return
	}
	p, ok := proxies[nw.ProxyName]
	if !ok {
		util.Warnf("proxy %q tidak dikenal di client", nw.ProxyName)
		return
	}

	local, err := transport.Dial(p.LocalAddr, p.LocalPort)
	if err != nil {
		util.Warnf("dial local %s:%d: %v", p.LocalAddr, p.LocalPort, err)
		return
	}
	defer local.Close()

	util.Infof("work %q ↔ %s:%d", p.Name, p.LocalAddr, p.LocalPort)
	util.Bridge(stream, local)
}
