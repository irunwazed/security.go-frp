package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/irunwazed/tunnel/internal/config"
	"github.com/irunwazed/tunnel/internal/dashboard"
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
		util.Infof("vhost_http_port tidak diset; proxy http tidak tersedia")
	}

	mgr := proxy.NewManager(vhostLn, cfg.SubdomainHost)

	// Dashboard
	var dash *dashboard.Dashboard
	if cfg.DashboardPort > 0 {
		dash, err = dashboard.New(cfg.DashboardPort, cfg.DashboardDB)
		if err != nil {
			log.Fatalf("dashboard: %v", err)
		}
		defer dash.Close()
		go func() {
			if err := dash.Start(); err != nil {
				util.Warnf("dashboard berhenti: %v", err)
			}
		}()
	}

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
		go handleControlConn(ctx, conn, cfg, mgr, dash)
	}
}

func handleControlConn(
	ctx context.Context, conn net.Conn,
	cfg *config.ServerConfig, mgr *proxy.Manager,
	dash *dashboard.Dashboard,
) {
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

	clientName := login.ClientName
	if clientName == "" {
		clientName = runID
	}
	clientIP := extractIP(conn.RemoteAddr().String())
	clientHost := buildHostSummary(login.Proxies, cfg.SubdomainHost)

	// Status "process": auth OK, registrasi proxy sedang berjalan.
	if dash != nil {
		dash.ClientProcess(runID, clientName, clientIP, clientHost)
	}

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

	// Status "connect": semua proxy aktif.
	if dash != nil {
		dash.ClientConnect(runID)
	}

	util.Infof("client %s login OK | run_id=%s ver=%s proxies=%d",
		conn.RemoteAddr(), runID, login.Version, len(login.Proxies))
	logProxies(login.Proxies, cfg.SubdomainHost)

	select {
	case <-sess.CloseChan():
	case <-ctx.Done():
		_ = sess.Close()
	}

	// Status "disconnect": sesi berakhir.
	if dash != nil {
		dash.ClientDisconnect(runID)
	}
	mgr.Unregister(sess)
	util.Infof("client %s disconnect (run_id=%s)", conn.RemoteAddr(), runID)
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
				util.Infof("  proxy http %q → subdomain %q", p.Name,
					p.Subdomain+"."+subdomainHost)
			}
		}
	}
}

func buildHostSummary(proxies []protocol.ProxyEntry, subdomainHost string) string {
	var parts []string
	for _, p := range proxies {
		switch p.Type {
		case "tcp":
			parts = append(parts, fmt.Sprintf("tcp:%d", p.RemotePort))
		case "http":
			parts = append(parts, p.CustomDomains...)
			if p.Subdomain != "" && subdomainHost != "" {
				parts = append(parts, p.Subdomain+"."+subdomainHost)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func extractIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
