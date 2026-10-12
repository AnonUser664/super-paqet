//go:build linux && !spq_private_writers

// File private_output_disabled_linux.go keeps ordinary builds on the qualified
// shared-writer architecture. Private writers remain an unaccepted experiment.
package socket

// privateOutputEnabled is resolved at compile time, not in the packet hot path.
const privateOutputEnabled = false
