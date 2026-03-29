package cmd

import (
	"github.com/dlc-01/relayd/internal/client"
	"github.com/dlc-01/relayd/internal/config"
	"github.com/spf13/cobra"
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Start relayd client",
	RunE:  runClient,
}

func init() {
	clientCmd.Flags().String("server", "", "Control server address (RELAYD_SERVER_CONTROL)")
	clientCmd.Flags().String("data", "", "Data server address (RELAYD_SERVER_DATA)")
	clientCmd.Flags().String("token", "", "Auth token (RELAYD_TOKEN)")
	clientCmd.Flags().StringArray("tunnel", nil, "Tunnel definition, repeatable (RELAYD_TUNNELS)")
	clientCmd.Flags().String("pin-file", "", "Pin file path (RELAYD_PIN_FILE)")
	clientCmd.Flags().String("session-file", "", "Session file path (RELAYD_SESSION_FILE)")
	clientCmd.Flags().Bool("dev", false, "Development mode (RELAYD_DEV)")
}

func runClient(cmd *cobra.Command, args []string) error {
	bindEnv(cmd, "server", "RELAYD_SERVER_CONTROL")
	bindEnv(cmd, "data", "RELAYD_SERVER_DATA")
	bindEnv(cmd, "token", "RELAYD_TOKEN")
	bindEnv(cmd, "pin-file", "RELAYD_PIN_FILE")
	bindEnv(cmd, "session-file", "RELAYD_SESSION_FILE")
	bindEnvBool(cmd, "dev", "RELAYD_DEV")

	if tunnels, _ := cmd.Flags().GetStringArray("tunnel"); len(tunnels) > 0 {
		bindTunnels(tunnels)
	}

	cfg := config.LoadClientConfig()
	c := client.New(cfg)
	c.Run()
	return nil
}
