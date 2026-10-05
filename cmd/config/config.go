// File config.go: registers config validate as the shared strict-validation CLI.
package config

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"paqet/internal/engine"
)

// Cmd registers the configuration command tree for the production executable.
var Cmd = NewCommand()

// NewCommand creates independent flag state so validation can be tested and
// embedded without carrying flags from a previous execution.
func NewCommand() *cobra.Command {
	var path string
	var machine bool
	root := &cobra.Command{Use: "config", Short: "Inspect and validate tunnel configuration"}
	validate := &cobra.Command{
		Use: "validate", Short: "Validate YAML, endpoints and discovered network settings",
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := engine.Load(path)
			if machine {
				// Never serialize the prepared config: it contains resolved encryption keys.
				result := map[string]any{"valid": err == nil, "path": path}
				if err != nil {
					result["error"] = err.Error()
				} else {
					result["listeners"] = len(cfg.Listeners)
					result["peers"] = len(cfg.Peers)
					result["forwards"] = len(cfg.Forwards)
				}
				if outputErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); outputErr != nil {
					return outputErr
				}
			} else if err == nil {
				cmd.Printf("configuration valid: %s (%d listeners, %d peers, %d forwards)\n", path, len(cfg.Listeners), len(cfg.Peers), len(cfg.Forwards))
			}
			if err != nil {
				return fmt.Errorf("configuration invalid: %w", err)
			}
			return nil
		},
	}
	validate.Flags().StringVarP(&path, "config", "c", "config.yaml", "Path to the configuration file")
	validate.Flags().BoolVar(&machine, "json", false, "Print a JSON validation result for automation")
	root.AddCommand(validate)
	return root
}
