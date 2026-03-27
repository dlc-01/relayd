package config

import (
	"os"
	"strconv"
	"strings"
)

type ServerConfig struct {
	ControlAddr string
	DataAddr    string
	Dev         bool
}

type TunnelConfig struct {
	TunnelID   string
	PublicPort int
	LocalAddr  string
}

type ClientConfig struct {
	ServerControlAddr string
	ServerDataAddr    string
	Tunnels           []TunnelConfig
	Dev               bool
}

func LoadServerConfig() ServerConfig {
	return ServerConfig{
		ControlAddr: getEnv("RELAYD_CONTROL_ADDR", "0.0.0.0:7000"),
		DataAddr:    getEnv("RELAYD_DATA_ADDR", "0.0.0.0:7001"),
		Dev:         getEnv("RELAYD_DEV", "false") == "true",
	}
}

func LoadClientConfig() ClientConfig {
	var tunnels []TunnelConfig

	raw := getEnv("RELAYD_TUNNELS", "web:10001:127.0.0.1:8080")
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(entry, ":", 4)
		if len(parts) != 4 {
			continue
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		tunnels = append(tunnels, TunnelConfig{
			TunnelID:   parts[0],
			PublicPort: port,
			LocalAddr:  parts[2] + ":" + parts[3],
		})
	}

	return ClientConfig{
		ServerControlAddr: getEnv("RELAYD_SERVER_CONTROL", "localhost:7000"),
		ServerDataAddr:    getEnv("RELAYD_SERVER_DATA", "localhost:7001"),
		Tunnels:           tunnels,
		Dev:               getEnv("RELAYD_DEV", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
