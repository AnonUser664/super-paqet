// Package config exposes explicit preparation-only validation. It never opens
// tunnel/forward sockets or installs firewall rules; missing discovery settings
// may invoke the same neighbor discovery used by startup and reload.
package config
