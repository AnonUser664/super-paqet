//go:build linux && spq_private_writers

// File private_output_enabled_linux.go selects the explicitly requested local
// private-writer experiment. The tag does not establish production acceptance.
package socket

// privateOutputEnabled enables independent output descriptors for incoming raw
// carriers. Generic transports and movable client slots retain their old output.
const privateOutputEnabled = true
