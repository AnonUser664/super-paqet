// File main.go: registers the production CLI commands; one engine binary can listen and
// forward together.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"paqet/cmd/config"
	"paqet/cmd/dump"
	"paqet/cmd/firewall"
	"paqet/cmd/ping"
	"paqet/cmd/run"
	"paqet/cmd/secret"
	"paqet/cmd/version"
)

// rootCmd registers the unified executable command tree rather than separate client/server
// programs.
var rootCmd = &cobra.Command{
	Use:           "super-paqet",
	Short:         "KCP transport over raw TCP packet",
	SilenceErrors: true,
	SilenceUsage:  true,
	Long:          `paqet is a bidirectional packet-level proxy using KCP and raw socket transport with encryption.`,
}

// main dispatches CLI work and returns failures as a nonzero process outcome for automation.
func main() {
	rootCmd.AddCommand(config.Cmd)
	rootCmd.AddCommand(run.Cmd)
	rootCmd.AddCommand(dump.Cmd)
	rootCmd.AddCommand(ping.Cmd)
	rootCmd.AddCommand(secret.Cmd)
	rootCmd.AddCommand(version.Cmd)
	rootCmd.AddCommand(firewall.Cmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
