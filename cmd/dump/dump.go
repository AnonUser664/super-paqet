package dump

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/spf13/cobra"
	"os/signal"
	"paqet/internal/engine"
	"paqet/internal/socket"
	"syscall"
)

var path string
var listener int

func init() {
	Cmd.Flags().StringVarP(&path, "config", "c", "config.yaml", "Configuration file")
	Cmd.Flags().IntVar(&listener, "listener", 0, "Listener index to capture")
}

var Cmd = &cobra.Command{Use: "dump", Short: "Captures raw TCP payloads for a configured listener", RunE: func(cmd *cobra.Command, args []string) error {
	cfg, err := engine.Load(path)
	if err != nil {
		return err
	}
	if listener < 0 || listener >= len(cfg.Listeners) {
		return fmt.Errorf("select a valid listener index")
	}
	n := cfg.Listeners[listener].Network
	conn, err := socket.New(&n)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	closed := context.AfterFunc(ctx, func() { conn.Close() })
	defer closed()
	buf := make([]byte, 65535)
	for {
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %d bytes\n%s", src, n, hex.Dump(buf[:n]))
	}
}}
