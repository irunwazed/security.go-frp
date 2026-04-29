package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// ServerConfig adalah konfigurasi untuk frps.
type ServerConfig struct {
	BindAddr      string `toml:"bind_addr"`
	BindPort      int    `toml:"bind_port"`
	VhostHTTPPort int    `toml:"vhost_http_port"`
	AuthToken     string `toml:"auth_token"`
	SubdomainHost string `toml:"subdomain_host"`
	DashboardPort int    `toml:"dashboard_port"`
	DashboardDB   string `toml:"dashboard_db"`
}

// ClientConfig adalah konfigurasi untuk frpc.
type ClientConfig struct {
	ServerAddr string        `toml:"server_addr"`
	ServerPort int           `toml:"server_port"`
	AuthToken  string        `toml:"auth_token"`
	ClientName string        `toml:"client_name"`
	Proxies    []ProxyConfig `toml:"proxies"`
}

// ProxyConfig adalah satu definisi proxy di sisi client.
// Type yang didukung: "tcp" (cocok untuk SSH) dan "http".
type ProxyConfig struct {
	Name          string   `toml:"name"`
	Type          string   `toml:"type"`
	LocalAddr     string   `toml:"local_addr"`
	LocalPort     int      `toml:"local_port"`
	RemotePort    int      `toml:"remote_port"`
	CustomDomains []string `toml:"custom_domains"`
	Subdomain     string   `toml:"subdomain"`
}

func LoadServer(path string) (*ServerConfig, error) {
	cfg := &ServerConfig{
		BindAddr:      "0.0.0.0",
		BindPort:      7000,
		DashboardPort: 7500,
		DashboardDB:   "frps.db",
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode server config: %w", err)
	}
	if cfg.BindPort == 0 {
		return nil, fmt.Errorf("bind_port wajib diisi")
	}
	return cfg, nil
}

func LoadClient(path string) (*ClientConfig, error) {
	cfg := &ClientConfig{}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode client config: %w", err)
	}
	if cfg.ServerAddr == "" || cfg.ServerPort == 0 {
		return nil, fmt.Errorf("server_addr dan server_port wajib diisi")
	}
	for i, p := range cfg.Proxies {
		if p.Name == "" {
			return nil, fmt.Errorf("proxies[%d].name kosong", i)
		}
		switch p.Type {
		case "tcp":
			if p.RemotePort == 0 {
				return nil, fmt.Errorf("proxy %q (tcp) butuh remote_port", p.Name)
			}
		case "http":
			if len(p.CustomDomains) == 0 && p.Subdomain == "" {
				return nil, fmt.Errorf("proxy %q (http) butuh custom_domains atau subdomain", p.Name)
			}
		default:
			return nil, fmt.Errorf("proxy %q tipe %q tidak didukung (gunakan tcp atau http)", p.Name, p.Type)
		}
		if p.LocalAddr == "" || p.LocalPort == 0 {
			return nil, fmt.Errorf("proxy %q butuh local_addr dan local_port", p.Name)
		}
	}
	return cfg, nil
}
