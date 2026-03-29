package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func bindEnv(cmd *cobra.Command, flag, env string) {
	if cmd.Flags().Changed(flag) {
		val, _ := cmd.Flags().GetString(flag)
		os.Setenv(env, val)
	} else if val := os.Getenv(env); val != "" {
		cmd.Flags().Set(flag, val)
	}
}

func bindEnvBool(cmd *cobra.Command, flag, env string) {
	if cmd.Flags().Changed(flag) {
		os.Setenv(env, "true")
	} else if val := os.Getenv(env); val == "true" {
		cmd.Flags().Set(flag, "true")
	}
}

func bindTunnels(tunnels []string) {
	if len(tunnels) > 0 {
		os.Setenv("RELAYD_TUNNELS", strings.Join(tunnels, ","))
	}
}
