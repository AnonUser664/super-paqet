// File firewall.go: exposes recovery of recorded dead-owner chains without flushing shared
// firewall tables.

package firewall

import (
	"github.com/spf13/cobra"
	"paqet/internal/engine"
)

// Cmd registers the firewall command, including its options and explicit error propagation.
var Cmd = &cobra.Command{Use: "firewall-cleanup", Short: "Removes journaled firewall rules belonging to dead tunnel processes in this namespace", RunE: func(cmd *cobra.Command, args []string) error { return engine.RecoverFirewall() }}
