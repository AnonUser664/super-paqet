// File run.go: loads the unified configuration and owns signal cancellation for one engine
// run.

package run

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"paqet/internal/engine"
)

// confPath selects the endpoint configuration used by the command.
var confPath string

// check selects preparation-only validation instead of binding sockets or installing rules.
var check bool

// init registers command options or initializes module-wide lookup/pool state once before
// runtime work begins.
func init() {
	Cmd.Flags().StringVarP(&confPath, "config", "c", "config.yaml", "Path to the configuration file")
	Cmd.Flags().BoolVar(&check, "check", false, "Validate configuration and discovered network settings, then exit")
}

// Cmd registers the run command, including its options and explicit error propagation.
var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Runs the Linux raw TCP tunnel and configured port forwards",
	Long:  `The 'run' command reads the specified YAML configuration file.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if check {
			if _, err := engine.Load(confPath); err != nil {
				return err
			}
			cmd.Println("configuration valid")
			return nil
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return engine.RunFile(ctx, confPath)
	},
}
