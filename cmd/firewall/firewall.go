package firewall

import (
	"github.com/spf13/cobra"
	"paqet/internal/engine"
)

var Cmd = &cobra.Command{Use: "firewall-cleanup", Short: "Removes journaled firewall rules belonging to dead tunnel processes in this namespace", RunE: func(cmd *cobra.Command, args []string) error { return engine.RecoverFirewall() }}
