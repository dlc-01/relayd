package main

import (
	"github.com/dlc-01/relayd/internal/client"
	"github.com/dlc-01/relayd/internal/config"
)

func main() {
	cfg := config.LoadClientConfig()
	client.New(cfg).Run()
}
