// File kcp.go: prepares lower-level KCP settings; enterprise endpoint defaults are applied
// earlier by engine configuration.

package conf

import (
	"fmt"
	"slices"
	"time"

	"github.com/xtaci/kcp-go/v5"
)

// KCP holds low-level reliability, packet, cipher and mux configuration; endpoint preparation
// supplies enterprise defaults first.
type KCP struct {
	// Maximum paced mux-frame duration in milliseconds; it bounds bulk work ahead of new
	// controls.
	WriteBatchMS int `yaml:"write_batch_ms"`
	// Maximum adaptive ACK deferral in milliseconds, separate from the chosen live delay.
	ACKDelayMaxMS int `yaml:"ack_delay_max_ms"`
	// Optional expedited cumulative credit; nil selects the default and reliable updates remain
	// required.
	CreditHints *bool `yaml:"credit_hints"`
	// Optional inner ACK timing used to distinguish peer scheduling from queue delay.
	ACKTimestamps *bool `yaml:"ack_timestamps"`
	// Independent configuration override for receive-window adaptation.
	AdaptiveBuffersOverride *bool `yaml:"adaptive_buffers"`
	// Prepared receive-window adaptation choice, not another YAML input.
	AdaptiveBuffers bool `yaml:"-"`
	// Fixed incoming packet-worker count established before conversations are accepted.
	PacketWorkers int `yaml:"-"`
	// Prepared listener conversation admission ceiling; outgoing pool limits belong to Endpoint.
	MaxSessions int `yaml:"-"`
	// Enables directional FIN semantics at both enterprise mux endpoints.
	HalfClose bool `yaml:"-"`
	// Preset selector; manual knobs are applied only for the manual mode.
	Mode string `yaml:"mode"`
	// Manual retry-floor/backoff mode, not a promise of zero network delay.
	NoDelay int `yaml:"nodelay"`
	// Manual protocol update interval in milliseconds; this is not the retransmission timeout.
	Interval int `yaml:"interval"`
	// Manual fast-gap evidence threshold, not a maximum retry count.
	Resend int `yaml:"resend"`
	// Manual native KCP congestion-window switch; enterprise pacing is a separate controller.
	NoCongestion int `yaml:"nocongestion"`
	// Allows output batching until an update rather than forcing every application write
	// immediately.
	WDelay bool `yaml:"wdelay"`
	// Requests immediate ACK behavior when batching/deferred scheduling does not require
	// coalescing.
	AckNoDelay bool `yaml:"acknodelay"`

	// Maximum inner transport datagram budget; cipher/FEC/core overhead consumes usable MSS.
	MTU int `yaml:"mtu"`
	// Receive capacity in segments, separate from outer TCP window and mux byte credit.
	Rcvwnd int `yaml:"rcvwnd"`
	// Send-window ceiling in segments; the live controller can use a smaller window.
	Sndwnd int `yaml:"sndwnd"`
	// FEC data-shard count; paired parity/data settings must agree across endpoints.
	Dshard int `yaml:"dshard"`
	// FEC parity-shard count; redundancy costs extra wire bytes and coding work.
	Pshard int `yaml:"pshard"`

	// Highest-priority configured cipher name; the trailing underscore distinguishes its parsed
	// object.
	Block_ string `yaml:"block"`
	// Configuration alias for cipher mode; precedence is resolved before transport construction.
	Enc string `yaml:"enc"`
	// Shared secret supplied by configuration/environment, never diagnostic payload.
	Key string `yaml:"key"`

	// Aggregate mux receive budget per carrier, in bytes rather than per idle stream
	// preallocation.
	Smuxbuf int `yaml:"smuxbuf"`
	// Per-stream advertised receive-window ceiling in bytes.
	Streambuf int `yaml:"streambuf"`

	// Configured keepalive interval in integer seconds.
	Smuxkalive_ int `yaml:"smuxkalive"`
	// Configured no-inbound-traffic timeout in integer seconds.
	Smuxktimeout_ int `yaml:"smuxktimeout"`

	// Prepared keepalive duration used when the mux session is constructed.
	Smuxkalive time.Duration `yaml:"-"`
	// Prepared no-inbound-traffic duration used by mux keepalive handling.
	Smuxktimeout time.Duration `yaml:"-"`
	// Prepared packet cipher; nil selects the no-envelope null path.
	Block kcp.BlockCrypt `yaml:"-"`
}

// setDefaults fills absent transport values after endpoint preparation; it preserves role-
// specific legacy windows when the engine did not override them.
func (k *KCP) setDefaults(role string) {
	if k.WriteBatchMS == 0 {
		k.WriteBatchMS = 20
	}
	if k.ACKDelayMaxMS == 0 {
		k.ACKDelayMaxMS = 20
	}
	if k.Mode == "" {
		k.Mode = "fast"
	}
	if k.MTU == 0 {
		k.MTU = 1350
	}

	if k.Rcvwnd == 0 {
		if role == "server" {
			k.Rcvwnd = 1024
		} else {
			k.Rcvwnd = 512
		}
	}
	if k.Sndwnd == 0 {
		if role == "server" {
			k.Sndwnd = 1024
		} else {
			k.Sndwnd = 128
		}
	}

	// if k.Dshard == 0 {
	// 	k.Dshard = 10
	// }
	// if k.Pshard == 0 {
	// 	k.Pshard = 3
	// }

	if k.Block_ == "" && k.Enc != "" {
		k.Block_ = k.Enc
	}
	if k.Block_ == "" {
		k.Block_ = "aes"
	}

	if k.Smuxbuf == 0 {
		k.Smuxbuf = 4 * 1024 * 1024
	}
	if k.Streambuf == 0 {
		k.Streambuf = 2 * 1024 * 1024
	}

	if k.Smuxkalive_ == 0 {
		k.Smuxkalive_ = 2
	}
	if k.Smuxktimeout_ == 0 {
		k.Smuxktimeout_ = 8
	}
}

// validate checks supported modes, windows, cipher/key and receive budgets before creating a
// carrier.
func (k *KCP) validate() []error {
	var errors []error

	validModes := []string{"normal", "fast", "fast2", "fast3", "manual"}
	if !slices.Contains(validModes, k.Mode) {
		errors = append(errors, fmt.Errorf("KCP mode must be one of: %v", validModes))
	}

	if k.MTU < 50 || k.MTU > 1500 {
		errors = append(errors, fmt.Errorf("KCP MTU must be between 50-1500 bytes"))
	}

	if k.Rcvwnd < 1 || k.Rcvwnd > 32768 {
		errors = append(errors, fmt.Errorf("KCP rcvwnd must be between 1-32768"))
	}
	if k.Sndwnd < 1 || k.Sndwnd > 32768 {
		errors = append(errors, fmt.Errorf("KCP sndwnd must be between 1-32768"))
	}

	validBlocks := []string{"aes", "aes-128", "aes-128-gcm", "aes-192", "salsa20", "blowfish", "twofish", "cast5", "3des", "tea", "xtea", "xor", "sm4", "none", "null"}
	if !slices.Contains(validBlocks, k.Block_) {
		errors = append(errors, fmt.Errorf("KCP encryption block must be one of: %v", validBlocks))
	}
	if !slices.Contains([]string{"none", "null"}, k.Block_) && len(k.Key) == 0 {
		errors = append(errors, fmt.Errorf("KCP encryption key is required"))
	}
	b, err := newBlock(k.Block_, k.Key)
	if err != nil {
		errors = append(errors, err)
	}
	k.Block = b

	if k.Smuxbuf < 1024 {
		errors = append(errors, fmt.Errorf("KCP smuxbuf must be >= 1024 bytes"))
	}
	if k.Streambuf < 1024 {
		errors = append(errors, fmt.Errorf("KCP streambuf must be >= 1024 bytes"))
	}

	k.Smuxkalive = time.Duration(k.Smuxkalive_) * time.Second
	k.Smuxktimeout = time.Duration(k.Smuxktimeout_) * time.Second

	return errors
}
