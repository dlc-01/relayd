package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func bindEnv(cmd *cobra.Command, flag, env string) {
	if !cmd.Flags().Changed(flag) {
		if val := os.Getenv(env); val != "" {
			cmd.Flags().Set(flag, val)
		}
	}
}

func bindEnvBool(cmd *cobra.Command, flag, env string) {
	if !cmd.Flags().Changed(flag) {
		if val := os.Getenv(env); val == "true" {
			cmd.Flags().Set(flag, "true")
		}
	}
}

func bindTunnels(tunnels []string) {
	if os.Getenv("RELAYD_TUNNELS") == "" {
		os.Setenv("RELAYD_TUNNELS", strings.Join(tunnels, ","))
	}
}
