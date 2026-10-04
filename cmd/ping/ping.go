package ping

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"paqet/internal/engine"
	"time"
)

var confPath, peer string

func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "config.yaml", "Configuration file")
	Cmd.Flags().StringVarP(&peer, "peer", "p", "", "Named peer (required if more than one)")
}

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
