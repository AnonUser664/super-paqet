package run

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"paqet/internal/engine"
)

var confPath string

var check bool

func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "config.yaml", "Path to the configuration file")
	Cmd.Flags().BoolVar(&check, "check", false, "Validate configuration and discovered network settings, then exit")
}

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Runs the Linux raw TCP tunnel and configured port forwards",
	Long:  `The 'run' command reads the specified YAML configuration file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := engine.Load(confPath)
		if err != nil {
			return err
		}
		if check {
			cmd.Println("configuration valid")
			return nil
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return engine.Run(ctx, cfg)
	},
}
