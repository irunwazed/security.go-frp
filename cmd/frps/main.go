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

	"github.com/irunwazed/tunnel/internal/config"
	"github.com/irunwazed/tunnel/internal/mux"
	"github.com/irunwazed/tunnel/internal/protocol"
	"github.com/irunwazed/tunnel/internal/proxy"
	"github.com/irunwazed/tunnel/internal/transport"
	"github.com/irunwazed/tunnel/pkg/util"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: frps <config.toml>")
		os.Exit(2)
	}

	cfg, err := config.LoadServer(os.Args[1])
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctrlLn, err := transport.Listen(cfg.BindAddr, cfg.BindPort)
	if err != nil {
		log.Fatalf("listen kontrol %s:%d: %v", cfg.BindAddr, cfg.BindPort, err)
	}
	util.Infof("control listen di %s", ctrlLn.Addr())

	var vhostLn net.Listener
	if cfg.VhostHTTPPort > 0 {
		vhostLn, err = transport.Listen(cfg.BindAddr, cfg.VhostHTTPPort)
		if err != nil {
			log.Fatalf("listen vhost %s:%d: %v", cfg.BindAddr, cfg.VhostHTTPPort, err)
		}
	} else {
		util.Infof("vhost_http_port tidak diset; proxy http tidak akan bisa diregister")
	}

	mgr := proxy.NewManager(vhostLn, cfg.SubdomainHost)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		util.Infof("shutting down")
		cancel()
		_ = ctrlLn.Close()
		if vhostLn != nil {
			_ = vhostLn.Close()
		}
	}()

	for {
		conn, err := ctrlLn.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			util.Warnf("accept kontrol: %v", err)
			continue
		}
		go handleControlConn(ctx, conn, cfg, mgr)
	}
}

func logProxies(proxies []protocol.ProxyEntry, subdomainHost string) {
	for _, p := range proxies {
		switch p.Type {
		case "tcp":
			util.Infof("  proxy tcp  %q → remote_port :%d", p.Name, p.RemotePort)
		case "http":
			for _, d := range p.CustomDomains {
				util.Infof("  proxy http %q → custom_domain %q", p.Name, d)
			}
			if p.Subdomain != "" {
				full := p.Subdomain + "." + subdomainHost
				util.Infof("  proxy http %q → subdomain %q", p.Name, full)
			}
		}
	}
}

func handleControlConn(ctx context.Context, conn net.Conn, cfg *config.ServerConfig, mgr *proxy.Manager) {
	util.Infof("client baru: %s", conn.RemoteAddr())

	sess, err := mux.Server(conn)
	if err != nil {
		util.Warnf("yamux server: %v", err)
		_ = conn.Close()
		return
	}

	ctrlStream, err := sess.AcceptStream()
	if err != nil {
		util.Warnf("accept control stream: %v", err)
		_ = sess.Close()
		return
	}

	var login protocol.Login
	mt, err := protocol.ReadMsg(ctrlStream, &login)
	if err != nil {
		util.Warnf("baca login: %v", err)
		_ = sess.Close()
		return
	}
	if mt != protocol.TypeLogin {
		_ = protocol.WriteMsg(ctrlStream, protocol.TypeLoginResp,
			&protocol.LoginResp{OK: false, Error: "expected login"})
		_ = sess.Close()
		return
	}

	if cfg.AuthToken != "" && login.Token != cfg.AuthToken {
		_ = protocol.WriteMsg(ctrlStream, protocol.TypeLoginResp,
			&protocol.LoginResp{OK: false, Error: "auth gagal"})
		util.Warnf("auth gagal dari %s", conn.RemoteAddr())
		_ = sess.Close()
		return
	}

	runID := util.RandomID()

	if err := mgr.Register(sess, login.Proxies); err != nil {
		_ = protocol.WriteMsg(ctrlStream, protocol.TypeLoginResp,
			&protocol.LoginResp{OK: false, Error: err.Error()})
		util.Warnf("register proxy: %v", err)
		_ = sess.Close()
		return
	}

	if err := protocol.WriteMsg(ctrlStream, protocol.TypeLoginResp,
		&protocol.LoginResp{OK: true, RunID: runID}); err != nil {
		util.Warnf("kirim login resp: %v", err)
		mgr.Unregister(sess)
		_ = sess.Close()
		return
	}

	util.Infof("client %s login OK | run_id=%s ver=%s proxies=%d",
		conn.RemoteAddr(), runID, login.Version, len(login.Proxies))
	logProxies(login.Proxies, cfg.SubdomainHost)

	// Tunggu sesi tutup, baik karena client disconnect maupun shutdown.
	select {
	case <-sess.CloseChan():
	case <-ctx.Done():
		_ = sess.Close()
	}
	mgr.Unregister(sess)
	util.Infof("client %s disconnect", conn.RemoteAddr())
}
