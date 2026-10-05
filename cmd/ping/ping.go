// File ping.go: probes a named peer through KCP without assuming control success proves
// application throughput.

package ping

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"paqet/internal/engine"
	"time"
)

// confPath select the config and named outgoing endpoint for a temporary peer probe.
var confPath, peer string

// init registers command options or initializes module-wide lookup/pool state once before
// runtime work begins.
func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "config.yaml", "Configuration file")
	Cmd.Flags().StringVarP(&peer, "peer", "p", "", "Named peer (required if more than one)")
}

// Cmd registers the ping command, including its options and explicit error propagation.
var Cmd = &cobra.Command{Use: "ping", Short: "Checks encrypted KCP delivery and response from a peer", RunE: func(cmd *cobra.Command, args []string) error {
	cfg, err := engine.Load(confPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Limits.OpenDuration)
	defer cancel()
	start := time.Now()
	if err := engine.Ping(ctx, cfg, peer); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pong %s\n", time.Since(start))
	return nil
}}
