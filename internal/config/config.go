package config

import (
	"os"
	"strconv"
	"strings"
)

type ServerConfig struct {
	ControlAddr   string
	DataAddr      string
	HTTPAddr      string
	TLSAddr       string
	TLSCertFile   string
	TLSKeyFile    string
	TLSDomain     string
	TLSDomains    []DomainCert
	MinPublicPort int
	MaxPublicPort int
	Dev           bool
}

type TunnelConfig struct {
	TunnelID   string
	PublicPort int
	LocalAddr  string
	Host       string
}

type ClientConfig struct {
	ServerControlAddr string
	ServerDataAddr    string
	Tunnels           []TunnelConfig
	Dev               bool
}

type DomainCert struct {
	Domain   string
	CertFile string
	KeyFile  string
}

func LoadServerConfig() ServerConfig {
	cfg := ServerConfig{
		ControlAddr:   getEnv("RELAYD_CONTROL_ADDR", "0.0.0.0:7000"),
		DataAddr:      getEnv("RELAYD_DATA_ADDR", "0.0.0.0:7001"),
		HTTPAddr:      getEnv("RELAYD_HTTP_ADDR", "0.0.0.0:80"),
		TLSAddr:       getEnv("RELAYD_TLS_ADDR", "0.0.0.0:443"),
		TLSCertFile:   getEnv("RELAYD_TLS_CERT", ""),
		TLSKeyFile:    getEnv("RELAYD_TLS_KEY", ""),
		TLSDomain:     getEnv("RELAYD_TLS_DOMAIN", "localhost"),
		MinPublicPort: getEnvInt("RELAYD_MIN_PORT", 10000),
		MaxPublicPort: getEnvInt("RELAYD_MAX_PORT", 60000),
		Dev:           getEnv("RELAYD_DEV", "false") == "true",
	}

	if raw := getEnv("RELAYD_TLS_DOMAINS", ""); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			parts := strings.SplitN(entry, ":", 3)
			if len(parts) != 3 {
				continue
			}
			cfg.TLSDomains = append(cfg.TLSDomains, DomainCert{
				Domain:   parts[0],
				CertFile: parts[1],
				KeyFile:  parts[2],
			})
		}
	}

	return cfg
}

func LoadClientConfig() ClientConfig {
	var tunnels []TunnelConfig
	raw := getEnv("RELAYD_TUNNELS", "web:10001:127.0.0.1:8080")
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if t, ok := parseTunnel(entry); ok {
			tunnels = append(tunnels, t)
		}
	}
	return ClientConfig{
		ServerControlAddr: getEnv("RELAYD_SERVER_CONTROL", "localhost:7000"),
		ServerDataAddr:    getEnv("RELAYD_SERVER_DATA", "localhost:7001"),
		Tunnels:           tunnels,
		Dev:               getEnv("RELAYD_DEV", "false") == "true",
	}
}

func parseTunnel(entry string) (TunnelConfig, bool) {
	parts := strings.SplitN(entry, ":", 2)
	if len(parts) < 2 {
		return TunnelConfig{}, false
	}
	tunnelID := parts[0]
	rest := parts[1]

	if strings.HasPrefix(rest, "https:") {
		remainder := strings.TrimPrefix(rest, "https:")
		idx := strings.Index(remainder, ":")
		if idx < 0 {
			return TunnelConfig{}, false
		}
		host := remainder[:idx]
		localAddr := remainder[idx+1:]
		if host == "" || localAddr == "" {
			return TunnelConfig{}, false
		}
		return TunnelConfig{
			TunnelID:  tunnelID,
			Host:      host,
			LocalAddr: localAddr,
		}, true
	}

	if strings.HasPrefix(rest, "host:") {
		remainder := strings.TrimPrefix(rest, "host:")
		idx := strings.Index(remainder, ":")
		if idx < 0 {
			return TunnelConfig{}, false
		}
		host := remainder[:idx]
		localAddr := remainder[idx+1:]
		if host == "" || localAddr == "" {
			return TunnelConfig{}, false
		}
		return TunnelConfig{
			TunnelID:  tunnelID,
			Host:      host,
			LocalAddr: localAddr,
		}, true
	}

	portAndAddr := strings.SplitN(rest, ":", 3)
	if len(portAndAddr) != 3 {
		return TunnelConfig{}, false
	}
	port, err := strconv.Atoi(portAndAddr[0])
	if err != nil {
		return TunnelConfig{}, false
	}
	return TunnelConfig{
		TunnelID:   tunnelID,
		PublicPort: port,
		LocalAddr:  portAndAddr[1] + ":" + portAndAddr[2],
	}, true
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
