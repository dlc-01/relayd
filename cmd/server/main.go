package main

import (
	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/server"
)

func main() {
	cfg := config.LoadServerConfig()
	server.New(cfg).Run()
}
