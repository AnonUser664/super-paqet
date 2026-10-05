// File pcap.go: bounds capture/socket buffer requests; these buffers are separate from Go heap
// limits.

package conf

import (
	"fmt"
)

// PCAP bounds driver socket/capture storage separately from application and mux buffers.
type PCAP struct {
	// Requested driver receive/transmit storage in bytes, outside the Go heap budget.
	Sockbuf int `yaml:"sockbuf"`
}

// setDefaults chooses client/server capture budgets without allocating them per application
// stream.
func (p *PCAP) setDefaults(role string) {
	if p.Sockbuf == 0 {
		if role == "server" {
			p.Sockbuf = 8 * 1024 * 1024
		} else {
			p.Sockbuf = 4 * 1024 * 1024
		}
	}
}

// validate rejects unusable or excessive socket storage requests before handle activation.
func (p *PCAP) validate() []error {
	var errors []error

	if p.Sockbuf < 1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf must be >= 1024 bytes"))
	}

	if p.Sockbuf > 100*1024*1024 {
		errors = append(errors, fmt.Errorf("PCAP sockbuf too large (max 100MB)"))
	}

	return errors
}
