package cmd

import (
	"github.com/dlc-01/relayd/internal/config"
	"github.com/dlc-01/relayd/internal/server"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start relayd server",
	RunE:  runServer,
}

func init() {
	serverCmd.Flags().String("control", "", "Control address (RELAYD_CONTROL_ADDR)")
	serverCmd.Flags().String("data", "", "Data address (RELAYD_DATA_ADDR)")
	serverCmd.Flags().String("http", "", "HTTP address (RELAYD_HTTP_ADDR)")
	serverCmd.Flags().String("tls", "", "TLS address (RELAYD_TLS_ADDR)")
	serverCmd.Flags().String("token", "", "Master token (RELAYD_TOKEN)")
	serverCmd.Flags().String("tg-token", "", "Telegram bot token (RELAYD_TG_TOKEN)")
	serverCmd.Flags().String("tg-chat", "", "Telegram chat ID (RELAYD_TG_CHAT_ID)")
	serverCmd.Flags().String("session-ttl", "", "Session TTL e.g. 24h (RELAYD_SESSION_TTL)")
	serverCmd.Flags().Bool("dev", false, "Development mode (RELAYD_DEV)")
}

func runServer(cmd *cobra.Command, args []string) error {
	bindEnv(cmd, "control", "RELAYD_CONTROL_ADDR")
	bindEnv(cmd, "data", "RELAYD_DATA_ADDR")
	bindEnv(cmd, "http", "RELAYD_HTTP_ADDR")
	bindEnv(cmd, "tls", "RELAYD_TLS_ADDR")
	bindEnv(cmd, "token", "RELAYD_TOKEN")
	bindEnv(cmd, "tg-token", "RELAYD_TG_TOKEN")
	bindEnv(cmd, "tg-chat", "RELAYD_TG_CHAT_ID")
	bindEnv(cmd, "session-ttl", "RELAYD_SESSION_TTL")
	bindEnvBool(cmd, "dev", "RELAYD_DEV")

	cfg := config.LoadServerConfig()
	s := server.New(cfg)
	go s.ListenAdmin()
	s.Run()
	return nil
}
