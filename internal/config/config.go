package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ServerConfig struct {
	ControlAddr     string
	DataAddr        string
	HTTPAddr        string
	TLSAddr         string
	TLSCertFile     string
	TLSKeyFile      string
	TLSDomain       string
	TLSDomains      []DomainCert
	ControlCertFile string
	ControlKeyFile  string
	MinPublicPort   int
	MaxPublicPort   int
	PendingTimeout  time.Duration
	MasterToken     string
	SessionTTL      time.Duration
	AdminAddr       string
	Dev             bool
}

type TunnelConfig struct {
	TunnelID   string
	PublicPort int
	LocalAddr  string
	Host       string
	Hosts      []string
}

type ClientConfig struct {
	ServerControlAddr string
	ServerDataAddr    string
	Tunnels           []TunnelConfig
	PinFile           string
	Token             string
	SessionFile       string
	Dev               bool
}

type DomainCert struct {
	Domain   string
	CertFile string
	KeyFile  string
}

func LoadServerConfig() ServerConfig {
	cfg := ServerConfig{
		ControlAddr:     getEnv("RELAYD_CONTROL_ADDR", "0.0.0.0:7000"),
		DataAddr:        getEnv("RELAYD_DATA_ADDR", "0.0.0.0:7001"),
		HTTPAddr:        getEnv("RELAYD_HTTP_ADDR", "0.0.0.0:80"),
		TLSAddr:         getEnv("RELAYD_TLS_ADDR", "0.0.0.0:443"),
		TLSCertFile:     getEnv("RELAYD_TLS_CERT", ""),
		TLSKeyFile:      getEnv("RELAYD_TLS_KEY", ""),
		TLSDomain:       getEnv("RELAYD_TLS_DOMAIN", "localhost"),
		ControlCertFile: getEnv("RELAYD_CONTROL_CERT", "/opt/relayd/control.crt"),
		ControlKeyFile:  getEnv("RELAYD_CONTROL_KEY", "/opt/relayd/control.key"),
		MinPublicPort:   getEnvInt("RELAYD_MIN_PORT", 10000),
		MaxPublicPort:   getEnvInt("RELAYD_MAX_PORT", 60000),
		PendingTimeout:  getEnvDuration("RELAYD_PENDING_TIMEOUT", 30*time.Second),
		MasterToken:     getEnv("RELAYD_TOKEN", ""),
		SessionTTL:      getEnvDuration("RELAYD_SESSION_TTL", 24*time.Hour),
		AdminAddr:       getEnv("RELAYD_ADMIN_ADDR", "127.0.0.1:7002"),
		Dev:             getEnv("RELAYD_DEV", "false") == "true",
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

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
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
	home := os.Getenv("HOME")
	return ClientConfig{
		ServerControlAddr: getEnv("RELAYD_SERVER_CONTROL", "localhost:7000"),
		ServerDataAddr:    getEnv("RELAYD_SERVER_DATA", "localhost:7001"),
		Tunnels:           tunnels,
		PinFile:           getEnv("RELAYD_PIN_FILE", filepath.Join(home, ".relayd", "server.pin")),
		Token:             getEnv("RELAYD_TOKEN", ""),
		SessionFile:       getEnv("RELAYD_SESSION_FILE", filepath.Join(home, ".relayd", "session.json")),
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
		return parseHostTunnel(tunnelID, remainder)
	}

	if strings.HasPrefix(rest, "host:") {
		remainder := strings.TrimPrefix(rest, "host:")
		return parseHostTunnel(tunnelID, remainder)
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

func parseHostTunnel(tunnelID, remainder string) (TunnelConfig, bool) {
	lastColon := strings.LastIndex(remainder, ":")
	if lastColon < 0 {
		return TunnelConfig{}, false
	}
	port := remainder[lastColon+1:]
	beforePort := remainder[:lastColon]

	secondLastColon := strings.LastIndex(beforePort, ":")
	if secondLastColon < 0 {
		return TunnelConfig{}, false
	}
	ip := beforePort[secondLastColon+1:]
	hostsStr := beforePort[:secondLastColon]

	if hostsStr == "" || ip == "" || port == "" {
		return TunnelConfig{}, false
	}

	localAddr := ip + ":" + port
	hostParts := strings.Split(hostsStr, "|")

	tc := TunnelConfig{
		TunnelID:  tunnelID,
		LocalAddr: localAddr,
		Host:      hostParts[0],
	}
	if len(hostParts) > 1 {
		tc.Hosts = hostParts[1:]
	}

	return tc, true
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
