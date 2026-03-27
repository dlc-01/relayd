package config

import "os"

type ServerConfig struct {
	ControlAddr string
	DataAddr    string
	PublicAddr  string
}

type ClientConfig struct {
	ServerControlAddr string
	ServerDataAddr    string
	TunnelID          string
	LocalAddr         string
}

func LoadServerConfig() ServerConfig {
	return ServerConfig{
		ControlAddr: getEnv("RELAYD_CONTROL_ADDR", "0.0.0.0:7000"),
		DataAddr:    getEnv("RELAYD_DATA_ADDR", "0.0.0.0:7001"),
		PublicAddr:  getEnv("RELAYD_PUBLIC_ADDR", "0.0.0.0:8080"),
	}
}

func LoadClientConfig() ClientConfig {
	return ClientConfig{
		ServerControlAddr: getEnv("RELAYD_SERVER_CONTROL", "localhost:7000"),
		ServerDataAddr:    getEnv("RELAYD_SERVER_DATA", "localhost:7001"),
		TunnelID:          getEnv("RELAYD_TUNNEL_ID", "web"),
		LocalAddr:         getEnv("RELAYD_LOCAL_ADDR", "127.0.0.1:3000"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
